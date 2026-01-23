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

func TestParseAll_GitSources(t *testing.T) {
	tests := []struct {
		name           string
		dockerfile     string
		wantGitCount   int
		wantImageCount int
		wantGitURLs    []string
	}{
		{
			name:           "ADD with https git URL",
			dockerfile:     "FROM alpine:3.18\nADD https://github.com/owner/repo.git#v1.0.0 /app/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"https://github.com/owner/repo.git#v1.0.0"},
		},
		{
			name:           "ADD with https git URL and subdir",
			dockerfile:     "FROM alpine:3.18\nADD https://github.com/owner/repo.git#main:subdirectory /app/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"https://github.com/owner/repo.git#main:subdirectory"},
		},
		{
			name:           "ADD with git protocol",
			dockerfile:     "FROM alpine:3.18\nADD git://github.com/owner/repo#branch /app/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"git://github.com/owner/repo#branch"},
		},
		{
			name:           "ADD with git@ SSH format",
			dockerfile:     "FROM alpine:3.18\nADD git@github.com:owner/repo.git#v2.0.0 /app/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"git@github.com:owner/repo.git#v2.0.0"},
		},
		{
			name:           "ADD with ssh:// protocol",
			dockerfile:     "FROM alpine:3.18\nADD ssh://git@github.com/owner/repo.git#tag /app/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"ssh://git@github.com/owner/repo.git#tag"},
		},
		{
			name:           "ADD with git URL without fragment",
			dockerfile:     "FROM alpine:3.18\nADD https://github.com/owner/repo.git /app/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"https://github.com/owner/repo.git"},
		},
		{
			name:           "ADD with --checksum is skipped (git)",
			dockerfile:     "FROM alpine:3.18\nADD --checksum=sha256:abc123 https://github.com/owner/repo.git#main /app/",
			wantGitCount:   0,
			wantImageCount: 1,
			wantGitURLs:    nil,
		},
		{
			name:           "ADD with variable in git URL is skipped",
			dockerfile:     "FROM alpine:3.18\nARG TAG=v1.0.0\nADD https://github.com/owner/repo.git#${TAG} /app/",
			wantGitCount:   0,
			wantImageCount: 1,
			wantGitURLs:    nil,
		},
		{
			name:           "multiple git URLs in one ADD",
			dockerfile:     "FROM alpine:3.18\nADD https://github.com/a/b.git#main https://github.com/c/d.git#dev /app/",
			wantGitCount:   2,
			wantImageCount: 1,
			wantGitURLs:    []string{"https://github.com/a/b.git#main", "https://github.com/c/d.git#dev"},
		},
		{
			name:           "mixed git and HTTP URLs",
			dockerfile:     "FROM alpine:3.18\nADD https://github.com/owner/repo.git#v1.0.0 /src/\nADD https://example.com/file.txt /data/",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"https://github.com/owner/repo.git#v1.0.0"},
		},
		{
			name:           "https without .git is treated as HTTP, not git",
			dockerfile:     "FROM alpine:3.18\nADD https://example.com/path /app/",
			wantGitCount:   0,
			wantImageCount: 1,
			wantGitURLs:    nil,
		},
		{
			name:           "real-world cli/cli repository",
			dockerfile:     "FROM alpine:3.18\nADD https://github.com/cli/cli.git#v2.40.0 /cli-src",
			wantGitCount:   1,
			wantImageCount: 1,
			wantGitURLs:    []string{"https://github.com/cli/cli.git#v2.40.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAll(context.Background(), strings.NewReader(tt.dockerfile))
			if err != nil {
				t.Fatalf("ParseAll() error = %v", err)
			}

			if len(result.GitSources) != tt.wantGitCount {
				t.Errorf("ParseAll() returned %d Git sources, want %d", len(result.GitSources), tt.wantGitCount)
			}

			if len(result.Images) != tt.wantImageCount {
				t.Errorf("ParseAll() returned %d images, want %d", len(result.Images), tt.wantImageCount)
			}

			if tt.wantGitURLs != nil {
				for i, wantURL := range tt.wantGitURLs {
					if i >= len(result.GitSources) {
						t.Errorf("Missing Git source at index %d, want URL %q", i, wantURL)
						continue
					}
					if result.GitSources[i].URL != wantURL {
						t.Errorf("GitSources[%d].URL = %q, want %q", i, result.GitSources[i].URL, wantURL)
					}
				}
			}
		})
	}
}

func TestParseAll_CopyFrom(t *testing.T) {
	tests := []struct {
		name           string
		dockerfile     string
		wantImageCount int
		wantImages     []struct {
			original string
			domain   string
			path     string
			tag      string
		}
	}{
		{
			name:           "COPY --from with external image",
			dockerfile:     "FROM alpine:3.18\nCOPY --from=busybox:1.36 /bin/busybox /bin/",
			wantImageCount: 2,
			wantImages: []struct {
				original string
				domain   string
				path     string
				tag      string
			}{
				{original: "alpine:3.18", domain: "docker.io", path: "library/alpine", tag: "3.18"},
				{original: "busybox:1.36", domain: "docker.io", path: "library/busybox", tag: "1.36"},
			},
		},
		{
			name:           "COPY --from with ghcr.io image",
			dockerfile:     "FROM alpine:3.18\nCOPY --from=ghcr.io/myorg/myimage:v1.0.0 /app/bin /usr/local/bin/",
			wantImageCount: 2,
			wantImages: []struct {
				original string
				domain   string
				path     string
				tag      string
			}{
				{original: "alpine:3.18", domain: "docker.io", path: "library/alpine", tag: "3.18"},
				{original: "ghcr.io/myorg/myimage:v1.0.0", domain: "ghcr.io", path: "myorg/myimage", tag: "v1.0.0"},
			},
		},
		{
			name:           "COPY --from referencing build stage is skipped",
			dockerfile:     "FROM golang:1.21 AS builder\nRUN go build -o /app\nFROM alpine:3.18\nCOPY --from=builder /app /app",
			wantImageCount: 2,
			wantImages: []struct {
				original string
				domain   string
				path     string
				tag      string
			}{
				{original: "golang:1.21", domain: "docker.io", path: "library/golang", tag: "1.21"},
				{original: "alpine:3.18", domain: "docker.io", path: "library/alpine", tag: "3.18"},
			},
		},
		{
			name:           "COPY --from with stage index is skipped",
			dockerfile:     "FROM golang:1.21\nRUN go build -o /app\nFROM alpine:3.18\nCOPY --from=0 /app /app",
			wantImageCount: 2,
		},
		{
			name:           "COPY without --from is ignored",
			dockerfile:     "FROM alpine:3.18\nCOPY ./local /app/",
			wantImageCount: 1,
		},
		{
			name: "COPY --from with already digested image is skipped",
			dockerfile: `FROM alpine:3.18
COPY --from=busybox@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd /bin/busybox /bin/`,
			wantImageCount: 1,
		},
		{
			name:           "COPY --from with variable is skipped",
			dockerfile:     "FROM alpine:3.18\nARG BUILD_IMAGE=golang:1.21\nCOPY --from=${BUILD_IMAGE} /app /app",
			wantImageCount: 1,
		},
		{
			name: "multiple COPY --from instructions",
			dockerfile: `FROM alpine:3.18
COPY --from=busybox:1.36 /bin/busybox /bin/
COPY --from=nginx:1.25 /etc/nginx/nginx.conf /etc/nginx/`,
			wantImageCount: 3,
			wantImages: []struct {
				original string
				domain   string
				path     string
				tag      string
			}{
				{original: "alpine:3.18", domain: "docker.io", path: "library/alpine", tag: "3.18"},
				{original: "busybox:1.36", domain: "docker.io", path: "library/busybox", tag: "1.36"},
				{original: "nginx:1.25", domain: "docker.io", path: "library/nginx", tag: "1.25"},
			},
		},
		{
			name: "COPY --from case insensitive stage reference",
			dockerfile: `FROM golang:1.21 AS Builder
RUN go build -o /app
FROM alpine:3.18
COPY --from=builder /app /app`,
			wantImageCount: 2, // Builder stage reference should be skipped (case insensitive)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAll(context.Background(), strings.NewReader(tt.dockerfile))
			if err != nil {
				t.Fatalf("ParseAll() error = %v", err)
			}

			if len(result.Images) != tt.wantImageCount {
				got := make([]string, len(result.Images))
				for i, img := range result.Images {
					got[i] = img.Original
				}
				t.Fatalf("ParseAll() returned %d images %v, want %d", len(result.Images), got, tt.wantImageCount)
			}

			if tt.wantImages != nil {
				for i, want := range tt.wantImages {
					if i >= len(result.Images) {
						t.Errorf("Missing image at index %d, want %q", i, want.original)
						continue
					}
					got := result.Images[i]
					if got.Original != want.original {
						t.Errorf("Images[%d].Original = %q, want %q", i, got.Original, want.original)
					}
					if reference.Domain(got.Ref) != want.domain {
						t.Errorf("Images[%d] Domain = %q, want %q", i, reference.Domain(got.Ref), want.domain)
					}
					if reference.Path(got.Ref) != want.path {
						t.Errorf("Images[%d] Path = %q, want %q", i, reference.Path(got.Ref), want.path)
					}
					if want.tag != "" {
						tagged, ok := got.Ref.(reference.Tagged)
						if !ok {
							t.Errorf("Images[%d] expected tagged reference, got untagged", i)
						} else if tagged.Tag() != want.tag {
							t.Errorf("Images[%d] Tag = %q, want %q", i, tagged.Tag(), want.tag)
						}
					}
				}
			}
		})
	}
}

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https with .git suffix", "https://github.com/owner/repo.git", true},
		{"https with .git and fragment", "https://github.com/owner/repo.git#main", true},
		{"http with .git suffix", "http://example.com/repo.git", true},
		{"git protocol", "git://github.com/owner/repo", true},
		{"ssh protocol", "ssh://git@github.com/owner/repo.git", true},
		{"git@ SSH format", "git@github.com:owner/repo.git", true},
		{"https without .git", "https://example.com/file.txt", false},
		{"http without .git", "http://example.com/path", false},
		{"https with .git in middle", "https://example.com/repo.git/file.txt", false}, // .git must be at end
		{"local path", "./local/path", false},
		{"relative path", "path/to/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitURL(tt.url)
			if got != tt.want {
				t.Errorf("isGitURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
