package dockerfile

import (
	"context"
	"strings"
	"testing"

	"github.com/containers/image/v5/docker/reference"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantFirst  struct {
			original  string
			domain    string
			path      string
			tag       string
			stageName string
		}
	}{
		{
			name:       "simple alpine",
			dockerfile: "FROM alpine:3.18",
			wantCount:  1,
			wantFirst: struct {
				original  string
				domain    string
				path      string
				tag       string
				stageName string
			}{
				original: "alpine:3.18",
				domain:   "docker.io",
				path:     "library/alpine",
				tag:      "3.18",
			},
		},
		{
			name:       "multi-stage build",
			dockerfile: "FROM golang:1.21 AS builder\nFROM alpine:3.18",
			wantCount:  2,
			wantFirst: struct {
				original  string
				domain    string
				path      string
				tag       string
				stageName string
			}{
				original:  "golang:1.21",
				domain:    "docker.io",
				path:      "library/golang",
				tag:       "1.21",
				stageName: "builder",
			},
		},
		{
			name:       "ghcr.io image",
			dockerfile: "FROM ghcr.io/myorg/myimage:v1.0.0",
			wantCount:  1,
			wantFirst: struct {
				original  string
				domain    string
				path      string
				tag       string
				stageName string
			}{
				original: "ghcr.io/myorg/myimage:v1.0.0",
				domain:   "ghcr.io",
				path:     "myorg/myimage",
				tag:      "v1.0.0",
			},
		},
		{
			name:       "docker hub user image",
			dockerfile: "FROM myuser/myimage:latest",
			wantCount:  1,
			wantFirst: struct {
				original  string
				domain    string
				path      string
				tag       string
				stageName string
			}{
				original: "myuser/myimage:latest",
				domain:   "docker.io",
				path:     "myuser/myimage",
				tag:      "latest",
			},
		},
		{
			name:       "scratch is skipped",
			dockerfile: "FROM scratch",
			wantCount:  0,
		},
		{
			name:       "multi-stage FROM reference to previous stage is skipped",
			dockerfile: "FROM golang:1.21 AS builder\nFROM builder",
			wantCount:  1,
			wantFirst: struct {
				original  string
				domain    string
				path      string
				tag       string
				stageName string
			}{
				original:  "golang:1.21",
				domain:    "docker.io",
				path:      "library/golang",
				tag:       "1.21",
				stageName: "builder",
			},
		},
		{
			name:       "ARG variable in FROM is skipped",
			dockerfile: "ARG BASE_IMAGE=alpine:3.18\nFROM ${BASE_IMAGE}",
			wantCount:  0,
		},
		{
			name:       "already digested image is skipped",
			dockerfile: "FROM alpine@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := Parse(context.Background(), strings.NewReader(tt.dockerfile))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if len(refs) != tt.wantCount {
				t.Fatalf("Parse() returned %d refs, want %d", len(refs), tt.wantCount)
			}

			if tt.wantCount == 0 {
				return
			}

			got := refs[0]
			if got.Original != tt.wantFirst.original {
				t.Errorf("Original = %q, want %q", got.Original, tt.wantFirst.original)
			}
			if reference.Domain(got.Ref) != tt.wantFirst.domain {
				t.Errorf("Domain = %q, want %q", reference.Domain(got.Ref), tt.wantFirst.domain)
			}
			if reference.Path(got.Ref) != tt.wantFirst.path {
				t.Errorf("Path = %q, want %q", reference.Path(got.Ref), tt.wantFirst.path)
			}
			if tt.wantFirst.tag != "" {
				tagged, ok := got.Ref.(reference.Tagged)
				if !ok {
					t.Errorf("Expected tagged reference, got untagged")
				} else if tagged.Tag() != tt.wantFirst.tag {
					t.Errorf("Tag = %q, want %q", tagged.Tag(), tt.wantFirst.tag)
				}
			}
			if got.StageName != tt.wantFirst.stageName {
				t.Errorf("StageName = %q, want %q", got.StageName, tt.wantFirst.stageName)
			}
		})
	}
}

func TestParseAll_HTTPSources(t *testing.T) {
	tests := []struct {
		name           string
		dockerfile     string
		wantHTTPCount  int
		wantImageCount int
		wantHTTPURLs   []string
	}{
		{
			name:           "ADD with HTTP URL",
			dockerfile:     "FROM alpine:3.18\nADD https://example.com/file.txt /app/",
			wantHTTPCount:  1,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"https://example.com/file.txt"},
		},
		{
			name:           "ADD with HTTP URL (http scheme)",
			dockerfile:     "FROM alpine:3.18\nADD http://example.com/file.txt /app/",
			wantHTTPCount:  1,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"http://example.com/file.txt"},
		},
		{
			name:           "ADD with uppercase HTTPS scheme",
			dockerfile:     "FROM alpine:3.18\nADD HTTPS://example.com/file.txt /app/",
			wantHTTPCount:  1,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"HTTPS://example.com/file.txt"},
		},
		{
			name:           "ADD with mixed case Http scheme",
			dockerfile:     "FROM alpine:3.18\nADD Http://example.com/file.txt /app/",
			wantHTTPCount:  1,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"Http://example.com/file.txt"},
		},
		{
			name:           "ADD with --checksum is skipped",
			dockerfile:     "FROM alpine:3.18\nADD --checksum=sha256:abc123 https://example.com/file.txt /app/",
			wantHTTPCount:  0,
			wantImageCount: 1,
			wantHTTPURLs:   nil,
		},
		{
			name:           "ADD with local path is ignored",
			dockerfile:     "FROM alpine:3.18\nADD ./local/file.txt /app/",
			wantHTTPCount:  0,
			wantImageCount: 1,
			wantHTTPURLs:   nil,
		},
		{
			name:           "ADD with variable in URL is skipped",
			dockerfile:     "FROM alpine:3.18\nARG URL=https://example.com\nADD ${URL}/file.txt /app/",
			wantHTTPCount:  0,
			wantImageCount: 1,
			wantHTTPURLs:   nil,
		},
		{
			name:           "ADD with $VAR pattern is skipped",
			dockerfile:     "FROM alpine:3.18\nARG FILE=myfile.txt\nADD https://example.com/$FILE /app/",
			wantHTTPCount:  0,
			wantImageCount: 1,
			wantHTTPURLs:   nil,
		},
		{
			name:           "multiple HTTP URLs in one ADD",
			dockerfile:     "FROM alpine:3.18\nADD https://example.com/a.txt https://example.com/b.txt /app/",
			wantHTTPCount:  2,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"https://example.com/a.txt", "https://example.com/b.txt"},
		},
		{
			name:           "multiple ADD instructions",
			dockerfile:     "FROM alpine:3.18\nADD https://example.com/a.txt /app/\nADD https://example.com/b.txt /app/",
			wantHTTPCount:  2,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"https://example.com/a.txt", "https://example.com/b.txt"},
		},
		{
			name:           "mixed local and HTTP in ADD",
			dockerfile:     "FROM alpine:3.18\nADD ./local.txt https://example.com/remote.txt /app/",
			wantHTTPCount:  1,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"https://example.com/remote.txt"},
		},
		{
			name:           "no ADD instructions",
			dockerfile:     "FROM alpine:3.18\nRUN echo hello",
			wantHTTPCount:  0,
			wantImageCount: 1,
			wantHTTPURLs:   nil,
		},
		{
			name:           "COPY is not processed (only ADD)",
			dockerfile:     "FROM alpine:3.18\nCOPY https://example.com/file.txt /app/",
			wantHTTPCount:  0,
			wantImageCount: 1,
			wantHTTPURLs:   nil,
		},
		{
			name: "real-world GitHub raw content",
			dockerfile: `FROM alpine:3.18
ADD https://raw.githubusercontent.com/moby/moby/master/README.md /app/`,
			wantHTTPCount:  1,
			wantImageCount: 1,
			wantHTTPURLs:   []string{"https://raw.githubusercontent.com/moby/moby/master/README.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAll(context.Background(), strings.NewReader(tt.dockerfile))
			if err != nil {
				t.Fatalf("ParseAll() error = %v", err)
			}

			if len(result.HTTPSources) != tt.wantHTTPCount {
				t.Errorf("ParseAll() returned %d HTTP sources, want %d", len(result.HTTPSources), tt.wantHTTPCount)
			}

			if len(result.Images) != tt.wantImageCount {
				t.Errorf("ParseAll() returned %d images, want %d", len(result.Images), tt.wantImageCount)
			}

			if tt.wantHTTPURLs != nil {
				for i, wantURL := range tt.wantHTTPURLs {
					if i >= len(result.HTTPSources) {
						t.Errorf("Missing HTTP source at index %d, want URL %q", i, wantURL)
						continue
					}
					if result.HTTPSources[i].URL != wantURL {
						t.Errorf("HTTPSources[%d].URL = %q, want %q", i, result.HTTPSources[i].URL, wantURL)
					}
				}
			}
		})
	}
}
