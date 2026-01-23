// Package http provides an HTTP client for fetching checksums of remote resources
package http

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Client handles HTTP checksum operations
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new HTTP client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Allow time for large file downloads
		},
	}
}

// GetChecksum fetches the SHA256 checksum for a URL
// It attempts to use server-provided checksums when available to avoid downloading the entire file
func (c *Client) GetChecksum(ctx context.Context, rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// GitHub releases require a separate API call (can't detect from headers)
	if parsedURL.Host == "github.com" && strings.Contains(parsedURL.Path, "/releases/download/") {
		checksum, err := c.getChecksumFromGitHubRelease(ctx, parsedURL)
		if err == nil && checksum != "" {
			return checksum, nil
		}
		// Fall through to HEAD-based detection on API failure
	}

	// Try HEAD request first to detect server type and extract checksums without downloading
	checksum, err := c.getChecksumFromHEAD(ctx, rawURL)
	if err == nil && checksum != "" {
		return checksum, nil
	}

	// Fallback: download and compute SHA256
	return c.computeChecksum(ctx, rawURL)
}

// getChecksumFromHEAD makes a HEAD request and tries to extract checksum from response headers.
// It detects S3 from the Server header (more reliable than URL pattern matching) and handles
// various server-specific checksum formats.
func (c *Client) getChecksumFromHEAD(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", err
	}

	// Request S3 checksums if available (this header is ignored by non-S3 servers)
	req.Header.Set("x-amz-checksum-mode", "ENABLED")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD request failed: %s", resp.Status)
	}

	// Detect S3 from Server header (more reliable than URL pattern matching)
	server := resp.Header.Get("Server")
	if server == "AmazonS3" {
		return c.extractS3Checksum(resp)
	}

	// Check for raw.githubusercontent.com ETag pattern (SHA256 hash)
	etag := resp.Header.Get("ETag")
	etag = strings.Trim(etag, `"`)
	if len(etag) == 64 && isHexString(etag) {
		return "sha256:" + etag, nil
	}

	return "", fmt.Errorf("no usable checksum found in headers")
}

// extractS3Checksum extracts SHA-256 checksum from S3 response headers.
// BuildKit's http.checksum only supports SHA-256, so we only look for that.
// If unavailable, caller should fall back to downloading and computing SHA-256.
func (c *Client) extractS3Checksum(resp *http.Response) (string, error) {
	// Check for explicit S3 SHA-256 checksum (the only algorithm BuildKit supports)
	if sha256Checksum := resp.Header.Get("x-amz-checksum-sha256"); sha256Checksum != "" {
		// S3 returns base64-encoded checksum, convert to hex
		decoded, err := decodeBase64ToHex(sha256Checksum)
		if err == nil {
			return "sha256:" + decoded, nil
		}
	}

	// No SHA-256 available from headers - caller will fall back to computing it
	return "", fmt.Errorf("no SHA-256 checksum found in S3 headers")
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
		return "", fmt.Errorf("invalid GitHub release URL format")
	}

	owner := pathParts[0]
	repo := pathParts[1]
	// pathParts[2] = "releases"
	// pathParts[3] = "download"
	tag := pathParts[4]
	rawAssetName := strings.Join(pathParts[5:], "/")

	// URL-decode the asset name to match GitHub API response (which returns unencoded names)
	assetName, err := url.PathUnescape(rawAssetName)
	if err != nil {
		return "", fmt.Errorf("invalid asset name encoding: %w", err)
	}

	// Query the GitHub API for the release
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Support GITHUB_TOKEN for authenticated requests (5,000 req/hr vs 60 req/hr)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

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

// computeChecksum downloads the content and computes SHA256
func (c *Client) computeChecksum(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET request failed: %s", resp.Status)
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, resp.Body); err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
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
