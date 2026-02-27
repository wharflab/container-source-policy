package mcr

import (
	"errors"
	"testing"

	"github.com/containers/image/v5/docker/reference"
)

func TestCanMapToMCR(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Should map - official Docker Hub images
		{"short name", "alpine:3.18", true},
		{"short name no tag", "alpine", true},
		{"explicit library", "docker.io/library/alpine:3.18", true},
		{"node image", "node:20", true},
		{"golang image", "golang:1.21-alpine", true},

		// Should NOT map - non-library images
		{"docker.io org image", "docker.io/myorg/myimage:1.0", false},
		{"docker.io user image", "docker.io/someuser/app:latest", false},

		// Should NOT map - other registries
		{"ghcr.io", "ghcr.io/actions/runner:latest", false},
		{"quay.io", "quay.io/centos/centos:8", false},
		{"gcr.io", "gcr.io/distroless/static:nonroot", false},
		{"ecr", "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp:v1", false},

		// Should NOT map - MCR itself (avoid double mapping)
		{"mcr image", "mcr.microsoft.com/mirror/docker/library/alpine:3.18", false},

		// Should NOT map - dhi.io
		{"dhi.io image", "dhi.io/alpine:3.18", false},

		// Should NOT map - ECR Public
		{"ecr public image", "public.ecr.aws/docker/library/alpine:3.18", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.input)
			if err != nil {
				t.Fatalf("failed to parse reference %q: %v", tt.input, err)
			}

			got := CanMapToMCR(ref)
			if got != tt.expected {
				t.Errorf("CanMapToMCR(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMapToMCR(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"alpine with tag", "alpine:3.18", "mcr.microsoft.com/mirror/docker/library/alpine:3.18"},
		{"alpine no tag", "alpine", "mcr.microsoft.com/mirror/docker/library/alpine"},
		{"explicit library", "docker.io/library/alpine:3.18", "mcr.microsoft.com/mirror/docker/library/alpine:3.18"},
		{"node image", "node:20", "mcr.microsoft.com/mirror/docker/library/node:20"},
		{"golang alpine", "golang:1.21-alpine", "mcr.microsoft.com/mirror/docker/library/golang:1.21-alpine"},
		{"python slim", "python:3.12-slim", "mcr.microsoft.com/mirror/docker/library/python:3.12-slim"},
		{"ubuntu noble", "ubuntu:noble", "mcr.microsoft.com/mirror/docker/library/ubuntu:noble"},
		{
			"with digest",
			"docker.io/library/alpine@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			"mcr.microsoft.com/mirror/docker/library/alpine@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.input)
			if err != nil {
				t.Fatalf("failed to parse reference %q: %v", tt.input, err)
			}

			got, err := MapToMCR(ref)
			if err != nil {
				t.Fatalf("MapToMCR(%q) returned error: %v", tt.input, err)
			}
			if got == nil {
				t.Fatalf("MapToMCR(%q) returned nil", tt.input)
			}

			gotStr := got.String()
			if gotStr != tt.expected {
				t.Errorf("MapToMCR(%q) = %q, want %q", tt.input, gotStr, tt.expected)
			}
		})
	}
}

func TestMapToMCR_NotEligible(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"ghcr.io", "ghcr.io/actions/runner:latest"},
		{"docker.io org", "docker.io/myorg/myimage:1.0"},
		{"quay.io", "quay.io/centos/centos:8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.input)
			if err != nil {
				t.Fatalf("failed to parse reference %q: %v", tt.input, err)
			}

			got, err := MapToMCR(ref)
			if !errors.Is(err, ErrNotEligible) {
				t.Errorf("MapToMCR(%q) error = %v, want ErrNotEligible", tt.input, err)
			}
			if got != nil {
				t.Errorf("MapToMCR(%q) = %q, want nil", tt.input, got.String())
			}
		})
	}
}
