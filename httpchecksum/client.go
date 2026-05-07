// Package httpchecksum provides an HTTP client for computing checksums of remote resources
// with optimizations for common hosting providers (S3, GitHub, raw.githubusercontent.com).
//
// The client attempts to retrieve checksums without downloading full content when possible:
//   - AWS S3: Uses X-Amz-Checksum-Sha256 header
//   - GitHub Releases: Uses GitHub API to fetch asset digests
//   - raw.githubusercontent.com: Uses ETag header (SHA256)
//   - Other servers: Downloads and computes SHA256
//
// The client also validates HTTP cache headers to detect volatile content that should
// not be pinned for reproducible builds.
//
// Example usage:
//
//	client := httpchecksum.NewClient()
//	checksum, err := client.GetChecksum(ctx, "https://example.com/file.tar.gz")
//	if err != nil {
//	    // Handle error
//	}
//	// checksum is in format "sha256:..."
package httpchecksum

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/pquerna/cachecontrol/cacheobject"

	"github.com/wharflab/container-source-policy/internal/version"
)

// AuthError indicates an HTTP resource requires authentication
type AuthError struct {
	URL        string
	StatusCode int
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("authentication required for %s (HTTP %d)", e.URL, e.StatusCode)
}

// IsAuthError checks if an error is an authentication error
func IsAuthError(err error) bool {
	var authErr *AuthError
	return errors.As(err, &authErr)
}

// VolatileContentError indicates an HTTP resource has caching headers that suggest
// the content changes frequently or should not be cached, making pinning unreliable.
type VolatileContentError struct {
	URL    string
	Reason string
}

func (e *VolatileContentError) Error() string {
	return fmt.Sprintf("volatile content at %s (%s)", e.URL, e.Reason)
}

// IsVolatileContentError checks if an error indicates volatile content
func IsVolatileContentError(err error) bool {
	var volatileErr *VolatileContentError
	return errors.As(err, &volatileErr)
}

// ChecksumResult contains the checksum and metadata for an HTTP resource
type ChecksumResult struct {
	// Checksum is the SHA256 checksum in the format "sha256:..."
	Checksum string
	// Headers contains HTTP headers that should be included in the source policy
	// These are the request headers that the response varies by (from the Vary header)
	Headers map[string]string
}

// ProgressWriterFactory creates a progress writer for a download
// contentLength is the total size in bytes (-1 if unknown)
// The returned writer receives all downloaded bytes
type ProgressWriterFactory func(contentLength int64) io.Writer

// Client handles HTTP checksum operations
type Client struct {
	httpClient      *http.Client
	progressFactory ProgressWriterFactory
}

// NewClient creates a new HTTP client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Allow time for large file downloads
		},
	}
}

// WithProgressFactory returns a copy of the client with progress reporting enabled
// The factory is called when a download starts, receiving the content length
func (c *Client) WithProgressFactory(factory ProgressWriterFactory) *Client {
	return &Client{
		httpClient:      c.httpClient,
		progressFactory: factory,
	}
}

// GetChecksum fetches the SHA256 checksum for a URL
// It attempts to use server-provided checksums when available to avoid downloading the entire file
func (c *Client) GetChecksum(ctx context.Context, rawURL string) (string, error) {
	result, err := c.GetChecksumWithHeaders(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return result.Checksum, nil
}

// GetChecksumWithHeaders fetches the SHA256 checksum for a URL along with relevant HTTP headers
// It attempts to use server-provided checksums when available to avoid downloading the entire file
// Returns headers that should be included in the source policy based on the Vary response header
func (c *Client) GetChecksumWithHeaders(ctx context.Context, rawURL string) (*ChecksumResult, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// GitHub releases require a separate API call (can't detect from headers)
	if parsedURL.Host == "github.com" && strings.Contains(parsedURL.Path, "/releases/download/") {
		checksum, err := c.getChecksumFromGitHubRelease(ctx, parsedURL)
		if err == nil && checksum != "" {
			return &ChecksumResult{Checksum: checksum, Headers: make(map[string]string)}, nil
		}
		// Propagate auth errors immediately, don't fall through
		if IsAuthError(err) {
			return nil, err
		}
		// Fall through to HEAD-based detection on other failures
	}

	// Try HEAD request first to detect server type and extract checksums without downloading
	result, err := c.getChecksumFromHEADWithHeaders(ctx, rawURL)
	if err == nil && result.Checksum != "" {
		return result, nil
	}
	// Propagate auth errors and volatile content errors immediately
	if IsAuthError(err) || IsVolatileContentError(err) {
		return nil, err
	}

	// Fallback: download and compute SHA256
	return c.computeChecksumWithHeaders(ctx, rawURL)
}

// getChecksumFromHEADWithHeaders makes a HEAD request and tries to extract checksum from response headers.
// It also extracts headers that should be included in the source policy based on the Vary header.
func (c *Client) getChecksumFromHEADWithHeaders(ctx context.Context, rawURL string) (*ChecksumResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Set User-Agent to identify the tool making requests
	// Matches BuildKit's convention: "buildkit/{version}"
	req.Header.Set("User-Agent", version.UserAgent())

	// Request S3 checksums if available (this header is ignored by non-S3 servers)
	req.Header.Set("X-Amz-Checksum-Mode", "ENABLED")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle authentication errors gracefully
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &AuthError{URL: rawURL, StatusCode: resp.StatusCode}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HEAD request failed: %s", resp.Status)
	}

	// Check cacheability before processing - volatile content should not be pinned
	if err := checkCacheability(rawURL, resp.Header); err != nil {
		return nil, err
	}

	// Extract checksum
	var checksum string
	// Detect S3 from Server header (more reliable than URL pattern matching)
	server := resp.Header.Get("Server")
	if server == "AmazonS3" {
		checksum, err = c.extractS3Checksum(resp)
		if err != nil {
			return nil, err
		}
	} else {
		// Check for raw.githubusercontent.com ETag pattern (SHA256 hash)
		etag := resp.Header.Get("ETag")
		etag = strings.Trim(etag, `"`)
		if len(etag) == 64 && isHexString(etag) {
			checksum = "sha256:" + etag
		} else {
			return nil, errors.New("no usable checksum found in headers")
		}
	}

	// Extract headers based on Vary response header
	headers := extractVaryHeaders(req.Header, resp.Header)

	return &ChecksumResult{
		Checksum: checksum,
		Headers:  headers,
	}, nil
}

// getChecksumFromHEAD makes a HEAD request and tries to extract checksum from response headers.
// It detects S3 from the Server header (more reliable than URL pattern matching) and handles
// various server-specific checksum formats.
// This is a convenience wrapper around getChecksumFromHEADWithHeaders that discards header metadata.
func (c *Client) getChecksumFromHEAD(ctx context.Context, rawURL string) (string, error) {
	result, err := c.getChecksumFromHEADWithHeaders(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return result.Checksum, nil
}

// extractS3Checksum extracts SHA-256 checksum from S3 response headers.
// BuildKit's http.checksum only supports SHA-256, so we only look for that.
// If unavailable, caller should fall back to downloading and computing SHA-256.
func (c *Client) extractS3Checksum(resp *http.Response) (string, error) {
	// Check for explicit S3 SHA-256 checksum (the only algorithm BuildKit supports)
	if sha256Checksum := resp.Header.Get("X-Amz-Checksum-Sha256"); sha256Checksum != "" {
		// S3 returns base64-encoded checksum, convert to hex
		decoded, err := decodeBase64ToHex(sha256Checksum)
		if err == nil {
			return "sha256:" + decoded, nil
		}
	}

	// No SHA-256 available from headers - caller will fall back to computing it
	return "", errors.New("no SHA-256 checksum found in S3 headers")
}

// GitHubReleaseAsset represents a release asset from the GitHub API
type GitHubReleaseAsset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"` // Available since June 2025
}

// GitHubRelease represents a release from the GitHub API
type GitHubRelease struct {
	Assets []GitHubReleaseAsset `json:"assets"`
}

// getChecksumFromGitHubRelease uses the GitHub API to get the digest for a release asset
func (c *Client) getChecksumFromGitHubRelease(ctx context.Context, parsedURL *url.URL) (string, error) {
	// Parse the release URL: /owner/repo/releases/download/tag/asset
	// e.g., /cli/cli/releases/download/v2.50.0/gh_2.50.0_linux_amd64.tar.gz
	pathParts := strings.Split(strings.TrimPrefix(parsedURL.Path, "/"), "/")
	if len(pathParts) < 6 {
		return "", errors.New("invalid GitHub release URL format")
	}

	owner := pathParts[0]
	repo := pathParts[1]
	tag := pathParts[4]
	rawAssetName := strings.Join(pathParts[5:], "/")

	// URL-decode the asset name to match GitHub API response (which returns unencoded names)
	assetName, err := url.PathUnescape(rawAssetName)
	if err != nil {
		return "", fmt.Errorf("invalid asset name encoding: %w", err)
	}

	// Query the GitHub API for the release
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")

	// Support GITHUB_TOKEN for authenticated requests (5,000 req/hr vs 60 req/hr)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle authentication errors
	// Note: 404 is NOT treated as auth error because GitHub returns 404 for both:
	// - Private repos/releases without auth (hiding existence)
	// - Genuinely missing tags/assets on public repos
	// We can't distinguish these cases, so treat 404 as a real "not found" error
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &AuthError{URL: parsedURL.String(), StatusCode: resp.StatusCode}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API request failed: %s", resp.Status)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub API response: %w", err)
	}

	// Find the matching asset
	for _, asset := range release.Assets {
		if asset.Name == assetName && asset.Digest != "" {
			// Digest is already in format "sha256:..."
			return asset.Digest, nil
		}
	}

	return "", fmt.Errorf("asset %s not found or has no digest", assetName)
}

// computeChecksumWithHeaders downloads the content, computes SHA256, and extracts relevant headers
func (c *Client) computeChecksumWithHeaders(ctx context.Context, rawURL string) (*ChecksumResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Set User-Agent to identify the tool making requests
	req.Header.Set("User-Agent", version.UserAgent())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle authentication errors gracefully
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &AuthError{URL: rawURL, StatusCode: resp.StatusCode}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET request failed: %s", resp.Status)
	}

	// Check cacheability before computing checksum - volatile content should not be pinned
	if err := checkCacheability(rawURL, resp.Header); err != nil {
		return nil, err
	}

	hash := sha256.New()

	// Create writer chain: hash the content, optionally report progress
	var dst io.Writer = hash
	if c.progressFactory != nil {
		progressWriter := c.progressFactory(resp.ContentLength)
		dst = io.MultiWriter(hash, progressWriter)
	}

	n, err := io.Copy(dst, resp.Body)
	// Validate Content-Length if provided (-1 means not present, e.g., chunked encoding)
	// A mismatch indicates server misconfiguration or network issues - we shouldn't trust such sources for pinning
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		if err != nil {
			return nil, fmt.Errorf(
				"content length mismatch: server declared %d bytes but sent %d: %w",
				resp.ContentLength, n, err,
			)
		}
		return nil, fmt.Errorf("content length mismatch: server declared %d bytes but sent %d", resp.ContentLength, n)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	checksum := "sha256:" + hex.EncodeToString(hash.Sum(nil))

	// Extract headers based on Vary response header
	headers := extractVaryHeaders(req.Header, resp.Header)

	return &ChecksumResult{
		Checksum: checksum,
		Headers:  headers,
	}, nil
}

// computeChecksum downloads the content and computes SHA256
// This is a convenience wrapper around computeChecksumWithHeaders that discards header metadata.
func (c *Client) computeChecksum(ctx context.Context, rawURL string) (string, error) {
	result, err := c.computeChecksumWithHeaders(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return result.Checksum, nil
}

var hexStringRegex = regexp.MustCompile("^[0-9a-fA-F]+$")

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	return hexStringRegex.MatchString(s)
}

// decodeBase64ToHex decodes a base64 string to hex
func decodeBase64ToHex(b64 string) (string, error) {
	// Standard base64 decoding
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(decoded), nil
}

// checkCacheability examines HTTP response headers to detect content that should not be pinned
// because it's explicitly marked as non-cacheable or volatile.
//
// Uses github.com/pquerna/cachecontrol for RFC 7234 compliant header parsing.
//
// Returns nil if the content is safe to pin, or a VolatileContentError if pinning is unreliable.
//
// Checks:
//   - Cache-Control: no-store - content must never be cached
//   - Cache-Control: no-cache - content requires revalidation on every use
//   - Cache-Control: max-age=0 or s-maxage=0 - content is immediately stale
//   - Expires header with past date - content is already expired
//   - Pragma: no-cache - HTTP/1.0 compatibility directive
//
// Note: Short max-age values (e.g., 300 seconds) are NOT flagged as volatile because
// CDNs like raw.githubusercontent.com use short cache times for freshness validation,
// not because the content actually changes frequently. Pinning such content is still useful.
//
// Note: Cache-Control: private is NOT flagged - it indicates cache scope (no shared caches),
// not content volatility. The content at a given URL is still stable and reproducible.
func checkCacheability(rawURL string, headers http.Header) error {
	// Check Pragma header (HTTP/1.0 compatibility)
	pragma := headers.Get("Pragma")
	if strings.Contains(strings.ToLower(pragma), "no-cache") {
		return &VolatileContentError{URL: rawURL, Reason: "Pragma: no-cache"}
	}

	// Parse Cache-Control header using RFC 7234 compliant library
	cacheControl := headers.Get("Cache-Control")
	if cacheControl != "" {
		directives, err := cacheobject.ParseResponseCacheControl(cacheControl)
		if err != nil {
			// Malformed Cache-Control header - be lenient and allow pinning
			// This follows the robustness principle: accept what others send
			return nil //nolint:nilerr // intentionally ignoring parse errors to be lenient
		}

		// Check for explicit non-caching directives
		if directives.NoStore {
			return &VolatileContentError{URL: rawURL, Reason: "Cache-Control: no-store"}
		}
		if directives.NoCachePresent {
			return &VolatileContentError{URL: rawURL, Reason: "Cache-Control: no-cache"}
		}
		// Note: "private" is intentionally NOT checked - it indicates cache scope
		// (no shared/proxy caches), not content volatility

		// Check max-age=0 or s-maxage=0 (immediately stale)
		// DeltaSeconds is -1 when not present, 0 or positive when specified
		// s-maxage takes precedence over max-age for shared caches
		effectiveMaxAge := directives.MaxAge
		if directives.SMaxAge >= 0 {
			effectiveMaxAge = directives.SMaxAge
		}

		if effectiveMaxAge == 0 {
			return &VolatileContentError{URL: rawURL, Reason: "Cache-Control: max-age=0 (immediately stale)"}
		}
		// Note: Short but non-zero max-age values (e.g., max-age=300) are allowed
		// because CDNs commonly use them for freshness validation, not because
		// content actually changes frequently.
	}

	// Check Expires header - only flag if already expired
	// Short-but-future Expires values follow the same logic as max-age
	expires := headers.Get("Expires")
	if expires != "" {
		expiresTime, err := http.ParseTime(expires)
		if err == nil {
			if expiresTime.Before(time.Now()) {
				return &VolatileContentError{URL: rawURL, Reason: "Expires header indicates already expired content"}
			}
		}
		// Invalid Expires header format is ignored per RFC 7234
	}

	return nil
}

// extractVaryHeaders extracts request headers that should be included in the source policy
// based on the Vary response header. The Vary header indicates which request headers
// the response depends on, so we need to include those headers to ensure reproducible builds.
//
// According to RFC 7231 Section 7.1.4, the Vary header contains a comma-separated list
// of header field names that the response varies by. Special value "*" means the response
// varies by factors beyond request headers (e.g., time, client IP), which we cannot capture.
//
// Returns a map of lowercase header names to their values, suitable for inclusion in
// BuildKit source policy attributes with the "http.header." prefix.
func extractVaryHeaders(reqHeaders, respHeaders http.Header) map[string]string {
	varyHeader := respHeaders.Get("Vary")
	if varyHeader == "" {
		// No Vary header means response doesn't vary by request headers
		return make(map[string]string)
	}

	// Vary: * means the response varies by unpredictable factors
	// We cannot capture this, so we return empty headers
	// BuildKit will still use the checksum for validation
	if strings.TrimSpace(varyHeader) == "*" {
		return make(map[string]string)
	}

	headers := make(map[string]string)

	// Parse comma-separated header names from Vary
	for headerName := range strings.SplitSeq(varyHeader, ",") {
		headerName = strings.TrimSpace(headerName)
		if headerName == "" {
			continue
		}

		// Get the request header value (case-insensitive lookup)
		headerValue := reqHeaders.Get(headerName)
		if headerValue != "" {
			// Store with lowercase key for consistent policy format
			// BuildKit uses lowercase header names in attributes
			headers[strings.ToLower(headerName)] = headerValue
		}
	}

	return headers
}
