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
			if tagged, ok := got.Ref.(reference.Tagged); ok {
				if tagged.Tag() != tt.wantFirst.tag {
					t.Errorf("Tag = %q, want %q", tagged.Tag(), tt.wantFirst.tag)
				}
			}
			if got.StageName != tt.wantFirst.stageName {
				t.Errorf("StageName = %q, want %q", got.StageName, tt.wantFirst.stageName)
			}
		})
	}
}
