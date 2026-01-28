package httpchecksum

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIsHexString(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abc123", true},
		{"ABC123", true},
		{"abcdef0123456789", true},
		{"ABCDEF0123456789", true},
		{"", false}, // empty string doesn't match ^[0-9a-fA-F]+$ (requires at least one char)
		{"xyz", false},
		{"abc 123", false},
		{"abc-123", false},
		{"abc_123", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isHexString(tt.input)
			if got != tt.want {
				t.Errorf("isHexString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeBase64ToHex(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid base64",
			input:   "SGVsbG8gV29ybGQ=", // "Hello World"
			want:    "48656c6c6f20576f726c64",
			wantErr: false,
		},
		{
			name:    "SHA256 hash in base64",
			input:   "LCa0a2j/xo/5m0U8HTBBNBNCLXBkg7+g+YpeiGJm564=",
			want:    "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
			wantErr: false,
		},
		{
			name:    "invalid base64",
			input:   "not-valid-base64!!!",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBase64ToHex(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeBase64ToHex() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("decodeBase64ToHex() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeChecksum(t *testing.T) {
	// Create a test server that returns known content
	content := []byte("Hello, World!")
	expectedHash := sha256.Sum256(content)
	expectedChecksum := "sha256:" + hex.EncodeToString(expectedHash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	client := NewClient()
	checksum, err := client.computeChecksum(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("computeChecksum() error = %v", err)
	}

	if checksum != expectedChecksum {
		t.Errorf("computeChecksum() = %v, want %v", checksum, expectedChecksum)
	}
}

func TestComputeChecksum_ContentLengthMismatch(t *testing.T) {
	tests := []struct {
		name          string
		content       []byte
		contentLength int // -1 means don't set Content-Length (chunked)
		wantErr       bool
	}{
		{
			name:          "matching Content-Length",
			content:       []byte("Hello, World!"),
			contentLength: 13,
			wantErr:       false,
		},
		{
			name:          "no Content-Length (chunked encoding)",
			content:       []byte("Hello, World!"),
			contentLength: -1,
			wantErr:       false,
		},
		{
			// Go's http.Client catches this at the transport level with "unexpected EOF"
			// Our code adds defense-in-depth validation
			name:          "Content-Length too large (server sent fewer bytes)",
			content:       []byte("Hello"),
			contentLength: 100,
			wantErr:       true,
		},
		{
			// Go's http.Client catches this at the transport level with "unexpected EOF"
			// Our code adds defense-in-depth validation
			name:          "Content-Length too small (server sent more bytes)",
			content:       []byte("Hello, World!"),
			contentLength: 5,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.contentLength >= 0 {
					w.Header().Set("Content-Length", strconv.Itoa(tt.contentLength))
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(tt.content)
			}))
			defer server.Close()

			client := NewClient()
			_, err := client.computeChecksum(context.Background(), server.URL)

			if tt.wantErr {
				if err == nil {
					t.Errorf("computeChecksum() expected error for Content-Length mismatch, got nil")
				}
				// Accept either Go's transport-level error or our validation error
				// Both indicate an untrusted server, which is the behavior we want
			} else {
				if err != nil {
					t.Errorf("computeChecksum() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestGetChecksumFromHEAD(t *testing.T) {
	tests := []struct {
		name         string
		headers      map[string]string
		statusCode   int
		wantChecksum string
		wantErr      bool
	}{
		// Non-S3 servers with SHA256 ETag (like raw.githubusercontent.com)
		{
			name: "valid SHA256 ETag",
			headers: map[string]string{
				"ETag": "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantErr:      false,
		},
		{
			name: "valid SHA256 ETag with quotes",
			headers: map[string]string{
				"ETag": "\"abc123def456abc123def456abc123def456abc123def456abc123def456abcd\"",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantErr:      false,
		},
		{
			name: "invalid ETag (not SHA256) on non-S3",
			headers: map[string]string{
				"ETag": "not-a-sha256-hash",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "",
			wantErr:      true,
		},
		{
			name:         "server error",
			headers:      map[string]string{},
			statusCode:   http.StatusNotFound,
			wantChecksum: "",
			wantErr:      true,
		},
		// S3 servers (detected via Server: AmazonS3 header)
		{
			name: "S3 with SHA256 checksum header",
			headers: map[string]string{
				"Server":                "AmazonS3",
				"x-amz-checksum-sha256": "LCa0a2j/xo/5m0U8HTBBNBNCLXBkg7+g+YpeiGJm564=",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
			wantErr:      false,
		},
		{
			name: "S3 with SHA1 only falls back (BuildKit requires SHA256)",
			headers: map[string]string{
				"Server":              "AmazonS3",
				"x-amz-checksum-sha1": "Lve95gjOVATpfV8EL5X4nxwjKHE=",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "",
			wantErr:      true,
		},
		{
			name: "S3 with MD5 ETag falls back (BuildKit requires SHA256)",
			headers: map[string]string{
				"Server": "AmazonS3",
				"ETag":   "\"098f6bcd4621d373cade4e832627b4f6\"",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "",
			wantErr:      true,
		},
		{
			name: "S3 with no checksum headers",
			headers: map[string]string{
				"Server": "AmazonS3",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					t.Errorf("expected HEAD request, got %s", r.Method)
				}
				// Verify the checksum mode header is always sent (ignored by non-S3)
				if r.Header.Get("X-Amz-Checksum-Mode") != "ENABLED" {
					t.Error("expected x-amz-checksum-mode: ENABLED header")
				}
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClient()
			checksum, err := client.getChecksumFromHEAD(context.Background(), server.URL)

			if (err != nil) != tt.wantErr {
				t.Errorf("getChecksumFromHEAD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if checksum != tt.wantChecksum {
				t.Errorf("getChecksumFromHEAD() = %v, want %v", checksum, tt.wantChecksum)
			}
		})
	}
}

func TestGetChecksumFromGitHubRelease(t *testing.T) {
	tests := []struct {
		name         string
		path         string // URL path to simulate (e.g., /owner/repo/releases/download/tag/asset)
		responseCode int
		responseBody string
		wantChecksum string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "valid release with digest",
			path:         "/cli/cli/releases/download/v2.50.0/gh_2.50.0_linux_amd64.tar.gz",
			responseCode: http.StatusOK,
			responseBody: `{"assets":[{"name":"gh_2.50.0_linux_amd64.tar.gz","digest":"sha256:abc123"}]}`,
			wantChecksum: "sha256:abc123",
			wantErr:      false,
		},
		{
			name:         "percent-encoded asset name",
			path:         "/owner/repo/releases/download/v1.0.0/file%20name.tar.gz",
			responseCode: http.StatusOK,
			responseBody: `{"assets":[{"name":"file name.tar.gz","digest":"sha256:def456"}]}`,
			wantChecksum: "sha256:def456",
			wantErr:      false,
		},
		{
			name:         "asset not found in release",
			path:         "/cli/cli/releases/download/v2.50.0/nonexistent.tar.gz",
			responseCode: http.StatusOK,
			responseBody: `{"assets":[{"name":"other.tar.gz","digest":"sha256:xyz"}]}`,
			wantChecksum: "",
			wantErr:      true,
			errContains:  "not found",
		},
		{
			name:         "asset exists but no digest",
			path:         "/cli/cli/releases/download/v2.50.0/gh_2.50.0_linux_amd64.tar.gz",
			responseCode: http.StatusOK,
			responseBody: `{"assets":[{"name":"gh_2.50.0_linux_amd64.tar.gz","digest":""}]}`,
			wantChecksum: "",
			wantErr:      true,
			errContains:  "not found",
		},
		{
			name:         "invalid URL format (too few path parts)",
			path:         "/cli/cli/releases",
			responseCode: http.StatusOK,
			responseBody: `{}`,
			wantChecksum: "",
			wantErr:      true,
			errContains:  "invalid GitHub release URL format",
		},
		{
			name:         "API 404 error (genuine not found)",
			path:         "/cli/cli/releases/download/v2.50.0/gh.tar.gz",
			responseCode: http.StatusNotFound,
			responseBody: `{"message":"Not Found"}`,
			wantChecksum: "",
			wantErr:      true,
			errContains:  "GitHub API request failed",
		},
		{
			name:         "API returns invalid JSON",
			path:         "/cli/cli/releases/download/v2.50.0/gh.tar.gz",
			responseCode: http.StatusOK,
			responseBody: `not json`,
			wantChecksum: "",
			wantErr:      true,
			errContains:  "failed to decode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the test path as a URL
			testURL, err := url.Parse("https://github.com" + tt.path)
			if err != nil {
				t.Fatalf("failed to parse test URL: %v", err)
			}

			// For the invalid URL test, no HTTP call is made
			if tt.errContains == "invalid GitHub release URL format" {
				client := NewClient()
				_, err := client.getChecksumFromGitHubRelease(context.Background(), testURL)
				if err == nil {
					t.Error("expected error for invalid URL format")
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			// Use mock transport that intercepts HTTP calls and verifies headers
			customClient := &Client{
				httpClient: &http.Client{
					Transport: &mockTransport{
						handler: func(req *http.Request) (*http.Response, error) {
							// Verify correct headers are sent
							if req.Header.Get("Accept") != "application/vnd.github+json" {
								t.Error("expected Accept: application/vnd.github+json header")
							}
							if req.Header.Get("X-Github-Api-Version") != "2022-11-28" {
								t.Error("expected X-GitHub-Api-Version: 2022-11-28 header")
							}
							return &http.Response{
								StatusCode: tt.responseCode,
								Body:       io.NopCloser(strings.NewReader(tt.responseBody)),
								Header:     make(http.Header),
							}, nil
						},
					},
				},
			}

			checksum, err := customClient.getChecksumFromGitHubRelease(context.Background(), testURL)

			if (err != nil) != tt.wantErr {
				t.Errorf("getChecksumFromGitHubRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			}
			if checksum != tt.wantChecksum {
				t.Errorf("getChecksumFromGitHubRelease() = %v, want %v", checksum, tt.wantChecksum)
			}
		})
	}
}

// mockTransport is a custom http.RoundTripper for testing
type mockTransport struct {
	handler func(*http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

func TestGetChecksumFromGitHubRelease_RealAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real API test in short mode")
	}

	// Test against a real GitHub release that has digest field
	// Using cli/cli v2.85.0 which has stable digests
	client := NewClient()

	testURL, err := url.Parse("https://github.com/cli/cli/releases/download/v2.85.0/gh_2.85.0_checksums.txt")
	if err != nil {
		t.Fatalf("failed to parse test URL: %v", err)
	}
	checksum, err := client.getChecksumFromGitHubRelease(context.Background(), testURL)
	if err != nil {
		t.Fatalf("getChecksumFromGitHubRelease() error = %v", err)
	}

	expectedChecksum := "sha256:0648ada2d7670b150066d0947445db34dbf9d7ecbe0c535d8f9f4df0a752c948"
	if checksum != expectedChecksum {
		t.Errorf("getChecksumFromGitHubRelease() = %v, want %v", checksum, expectedChecksum)
	}
}

func TestGetChecksum_Fallback(t *testing.T) {
	// Test that GetChecksum falls back to computing checksum when HEAD returns no usable checksum
	content := []byte("test content")
	expectedHash := sha256.Sum256(content)
	expectedChecksum := "sha256:" + hex.EncodeToString(expectedHash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// Return OK but with no usable checksum headers
			w.WriteHeader(http.StatusOK)
			return
		}
		// GET request - return content for checksum computation
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	client := NewClient()
	checksum, err := client.GetChecksum(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("GetChecksum() error = %v", err)
	}

	if checksum != expectedChecksum {
		t.Errorf("GetChecksum() = %v, want %v", checksum, expectedChecksum)
	}
}

// TestGetChecksumWithHeaders_VaryHeader tests that the Vary header is properly handled
// and relevant request headers are extracted and returned
func TestGetChecksumWithHeaders_VaryHeader(t *testing.T) {
	content := []byte("test content")
	hash := sha256.New()
	hash.Write(content)
	expectedChecksum := "sha256:" + hex.EncodeToString(hash.Sum(nil))

	tests := []struct {
		name                 string
		varyHeader           string
		expectedHeadersCount int
		checkUserAgent       bool // whether to verify user-agent header is present
	}{
		{
			name:                 "Vary: User-Agent",
			varyHeader:           "User-Agent",
			expectedHeadersCount: 1,
			checkUserAgent:       true,
		},
		{
			name:                 "Vary: User-Agent, Accept-Encoding",
			varyHeader:           "User-Agent, Accept-Encoding",
			expectedHeadersCount: 1, // Only User-Agent is in request, Accept-Encoding is not
			checkUserAgent:       true,
		},
		{
			name:                 "Vary: * (unpredictable)",
			varyHeader:           "*",
			expectedHeadersCount: 0,
			checkUserAgent:       false,
		},
		{
			name:                 "No Vary header",
			varyHeader:           "",
			expectedHeadersCount: 0,
			checkUserAgent:       false,
		},
		{
			name:                 "Vary: Non-existent-Header (not in request)",
			varyHeader:           "Non-Existent-Header",
			expectedHeadersCount: 0,
			checkUserAgent:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.varyHeader != "" {
					w.Header().Set("Vary", tt.varyHeader)
				}
				if r.Method == http.MethodHead {
					// Return ETag for checksum
					w.Header().Set("ETag", `"`+hex.EncodeToString(hash.Sum(nil))+`"`)
					w.WriteHeader(http.StatusOK)
					return
				}
				// GET request
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			}))
			defer server.Close()

			client := NewClient()
			result, err := client.GetChecksumWithHeaders(context.Background(), server.URL)
			if err != nil {
				t.Fatalf("GetChecksumWithHeaders() error = %v", err)
			}

			if result.Checksum != expectedChecksum {
				t.Errorf("GetChecksumWithHeaders() checksum = %v, want %v", result.Checksum, expectedChecksum)
			}

			// Check headers count
			if len(result.Headers) != tt.expectedHeadersCount {
				t.Errorf(
					"GetChecksumWithHeaders() headers count = %d, want %d (headers: %v)",
					len(result.Headers),
					tt.expectedHeadersCount,
					result.Headers,
				)
			}

			// Check user-agent header if expected
			if tt.checkUserAgent {
				if _, ok := result.Headers["user-agent"]; !ok {
					t.Errorf("GetChecksumWithHeaders() missing user-agent header")
				}
			}
		})
	}
}

// TestCheckCacheability tests the checkCacheability function
func TestCheckCacheability(t *testing.T) {
	tests := []struct {
		name        string
		headers     http.Header
		wantErr     bool
		errContains string
	}{
		{
			name:    "no caching headers - OK",
			headers: http.Header{},
			wantErr: false,
		},
		{
			name: "long max-age - OK",
			headers: http.Header{
				"Cache-Control": []string{"max-age=86400"}, // 24 hours
			},
			wantErr: false,
		},
		{
			name: "short max-age - OK (CDNs use short cache times for freshness)",
			headers: http.Header{
				"Cache-Control": []string{"max-age=300"}, // 5 minutes (like raw.githubusercontent.com)
			},
			wantErr: false, // Short max-age is OK - content is still stable
		},
		{
			name: "no-store - volatile",
			headers: http.Header{
				"Cache-Control": []string{"no-store"},
			},
			wantErr:     true,
			errContains: "no-store",
		},
		{
			name: "no-cache - volatile",
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
			},
			wantErr:     true,
			errContains: "no-cache",
		},
		{
			name: "private - OK (indicates cache scope, not volatility)",
			headers: http.Header{
				"Cache-Control": []string{"private, max-age=3600"},
			},
			wantErr: false, // private is about cache scope, content is still stable
		},
		{
			name: "max-age=0 - volatile",
			headers: http.Header{
				"Cache-Control": []string{"max-age=0"},
			},
			wantErr:     true,
			errContains: "max-age=0",
		},
		{
			name: "s-maxage=0 overrides max-age - volatile",
			headers: http.Header{
				"Cache-Control": []string{"max-age=86400, s-maxage=0"},
			},
			wantErr:     true,
			errContains: "max-age=0",
		},
		{
			name: "Pragma: no-cache - volatile",
			headers: http.Header{
				"Pragma": []string{"no-cache"},
			},
			wantErr:     true,
			errContains: "Pragma",
		},
		{
			name: "expired Expires header - volatile",
			headers: http.Header{
				"Expires": []string{"Thu, 01 Jan 1970 00:00:00 GMT"},
			},
			wantErr:     true,
			errContains: "expired",
		},
		{
			name: "short Expires - OK (like short max-age)",
			headers: http.Header{
				"Expires": []string{time.Now().Add(5 * time.Minute).UTC().Format(http.TimeFormat)},
			},
			wantErr: false, // Short but future Expires is OK
		},
		{
			name: "invalid Expires header - ignored (OK)",
			headers: http.Header{
				"Expires": []string{"invalid-date"},
			},
			wantErr: false,
		},
		{
			name: "public with long max-age - OK",
			headers: http.Header{
				"Cache-Control": []string{"public, max-age=31536000"}, // 1 year
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCacheability("https://example.com/file.txt", tt.headers)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkCacheability() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			}
			if tt.wantErr && err != nil {
				if !IsVolatileContentError(err) {
					t.Errorf("expected VolatileContentError, got %T", err)
				}
			}
		})
	}
}

// TestGetChecksumWithHeaders_VolatileContent tests that volatile content is properly rejected
func TestGetChecksumWithHeaders_VolatileContent(t *testing.T) {
	content := []byte("test content")

	tests := []struct {
		name    string
		headers map[string]string
		wantErr bool
	}{
		{
			name: "no-store returns error",
			headers: map[string]string{
				"Cache-Control": "no-store",
			},
			wantErr: true,
		},
		{
			name: "max-age=0 returns error",
			headers: map[string]string{
				"Cache-Control": "max-age=0",
			},
			wantErr: true,
		},
		{
			name: "cacheable content returns checksum",
			headers: map[string]string{
				"Cache-Control": "max-age=86400",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			}))
			defer server.Close()

			client := NewClient()
			result, err := client.GetChecksumWithHeaders(context.Background(), server.URL)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetChecksumWithHeaders() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if !IsVolatileContentError(err) {
					t.Errorf("expected VolatileContentError, got %T: %v", err, err)
				}
			} else {
				if result == nil || result.Checksum == "" {
					t.Error("expected non-empty checksum result")
				}
			}
		})
	}
}

// TestExtractVaryHeaders tests the extractVaryHeaders function directly
func TestExtractVaryHeaders(t *testing.T) {
	tests := []struct {
		name           string
		reqHeaders     http.Header
		respHeaders    http.Header
		expectedResult map[string]string
	}{
		{
			name: "Single header in Vary",
			reqHeaders: http.Header{
				"User-Agent": []string{"test-agent"},
			},
			respHeaders: http.Header{
				"Vary": []string{"User-Agent"},
			},
			expectedResult: map[string]string{
				"user-agent": "test-agent",
			},
		},
		{
			name: "Multiple headers in Vary",
			reqHeaders: http.Header{
				"User-Agent":      []string{"test-agent"},
				"Accept-Encoding": []string{"gzip, deflate"},
			},
			respHeaders: http.Header{
				"Vary": []string{"User-Agent, Accept-Encoding"},
			},
			expectedResult: map[string]string{
				"user-agent":      "test-agent",
				"accept-encoding": "gzip, deflate",
			},
		},
		{
			name: "Vary: * returns empty",
			reqHeaders: http.Header{
				"User-Agent": []string{"test-agent"},
			},
			respHeaders: http.Header{
				"Vary": []string{"*"},
			},
			expectedResult: map[string]string{},
		},
		{
			name: "No Vary header",
			reqHeaders: http.Header{
				"User-Agent": []string{"test-agent"},
			},
			respHeaders:    http.Header{},
			expectedResult: map[string]string{},
		},
		{
			name:       "Header in Vary but not in request",
			reqHeaders: http.Header{},
			respHeaders: http.Header{
				"Vary": []string{"User-Agent"},
			},
			expectedResult: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVaryHeaders(tt.reqHeaders, tt.respHeaders)

			if len(result) != len(tt.expectedResult) {
				t.Errorf("extractVaryHeaders() returned %d headers, want %d", len(result), len(tt.expectedResult))
			}

			for key, expectedValue := range tt.expectedResult {
				if actualValue, ok := result[key]; !ok {
					t.Errorf("extractVaryHeaders() missing header %q", key)
				} else if actualValue != expectedValue {
					t.Errorf("extractVaryHeaders() header %q = %q, want %q", key, actualValue, expectedValue)
				}
			}
		})
	}
}
