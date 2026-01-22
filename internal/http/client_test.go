package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
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
			name: "S3 with SHA1 checksum header (fallback)",
			headers: map[string]string{
				"Server":              "AmazonS3",
				"x-amz-checksum-sha1": "Lve95gjOVATpfV8EL5X4nxwjKHE=",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "sha1:2ef7bde608ce5404e97d5f042f95f89f1c232871",
			wantErr:      false,
		},
		{
			name: "S3 with MD5 ETag for single-part upload",
			headers: map[string]string{
				"Server": "AmazonS3",
				"ETag":   "\"098f6bcd4621d373cade4e832627b4f6\"",
			},
			statusCode:   http.StatusOK,
			wantChecksum: "md5:098f6bcd4621d373cade4e832627b4f6",
			wantErr:      false,
		},
		{
			name: "S3 multipart ETag is skipped",
			headers: map[string]string{
				"Server": "AmazonS3",
				"ETag":   "\"098f6bcd4621d373cade4e832627b4f6-5\"",
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
				if r.Header.Get("x-amz-checksum-mode") != "ENABLED" {
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
