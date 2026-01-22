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
	"regexp"
	"strings"
)

// Client handles HTTP checksum operations
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new HTTP client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
	}
}

// GetChecksum fetches the SHA256 checksum for a URL
// It attempts to use server-provided checksums when available to avoid downloading the entire file
func (c *Client) GetChecksum(ctx context.Context, rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// Try optimized methods based on the host
	switch {
	case parsedURL.Host == "raw.githubusercontent.com":
		// raw.githubusercontent.com returns SHA256 in ETag header
		checksum, err := c.getChecksumFromRawGitHub(ctx, rawURL)
		if err == nil && checksum != "" {
			return checksum, nil
		}

	case parsedURL.Host == "github.com" && strings.Contains(parsedURL.Path, "/releases/download/"):
		// GitHub releases: use API to get digest
		checksum, err := c.getChecksumFromGitHubRelease(ctx, parsedURL)
		if err == nil && checksum != "" {
			return checksum, nil
		}

	case strings.HasSuffix(parsedURL.Host, ".amazonaws.com") || strings.HasSuffix(parsedURL.Host, ".s3."):
		// S3: try to get checksum from headers
		checksum, err := c.getChecksumFromS3(ctx, rawURL)
		if err == nil && checksum != "" {
			return checksum, nil
		}
	}

	// Fallback: download and compute SHA256
	return c.computeChecksum(ctx, rawURL)
}

// getChecksumFromRawGitHub extracts SHA256 from ETag header on raw.githubusercontent.com
func (c *Client) getChecksumFromRawGitHub(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD request failed: %s", resp.Status)
	}

	// ETag on raw.githubusercontent.com is the SHA256 hash (without quotes usually, but let's handle both)
	etag := resp.Header.Get("ETag")
	etag = strings.Trim(etag, `"`)

	// Verify it looks like a SHA256 hex string (64 chars)
	if len(etag) == 64 && isHexString(etag) {
		return "sha256:" + etag, nil
	}

	return "", fmt.Errorf("ETag is not a valid SHA256: %s", etag)
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
	assetName := strings.Join(pathParts[5:], "/")

	// Query the GitHub API for the release
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

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

// getChecksumFromS3 tries to get checksum from S3 headers
func (c *Client) getChecksumFromS3(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", err
	}

	// Request checksums from S3
	req.Header.Set("x-amz-checksum-mode", "ENABLED")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD request failed: %s", resp.Status)
	}

	// Check for explicit S3 checksums (preferred - these are reliable)
	if sha256Checksum := resp.Header.Get("x-amz-checksum-sha256"); sha256Checksum != "" {
		// S3 returns base64-encoded checksum, convert to hex
		decoded, err := decodeBase64ToHex(sha256Checksum)
		if err == nil {
			return "sha256:" + decoded, nil
		}
	}

	if sha1Checksum := resp.Header.Get("x-amz-checksum-sha1"); sha1Checksum != "" {
		decoded, err := decodeBase64ToHex(sha1Checksum)
		if err == nil {
			return "sha1:" + decoded, nil
		}
	}

	// Check ETag for single-part uploads (MD5)
	// Only use if it doesn't have a multipart suffix (-N)
	etag := resp.Header.Get("ETag")
	etag = strings.Trim(etag, `"`)
	if etag != "" && !strings.Contains(etag, "-") && len(etag) == 32 && isHexString(etag) {
		// This is likely an MD5 from a single-part upload
		// Note: MD5 is weaker than SHA256, but better than nothing
		return "md5:" + etag, nil
	}

	return "", fmt.Errorf("no usable checksum found in S3 headers")
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

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	matched, _ := regexp.MatchString("^[0-9a-fA-F]+$", s)
	return matched
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
