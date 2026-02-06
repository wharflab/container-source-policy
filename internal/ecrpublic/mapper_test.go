package ecrpublic

import (
	"errors"
	"testing"

	"github.com/containers/image/v5/docker/reference"
)

func TestCanMapToECRPublic(t *testing.T) {
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
		{"ecr private", "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp:v1", false},

		// Should NOT map - ECR Public itself (avoid double mapping)
		{"ecr public image", "public.ecr.aws/docker/library/alpine:3.18", false},

		// Should NOT map - dhi.io
		{"dhi.io image", "dhi.io/alpine:3.18", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.input)
			if err != nil {
				t.Fatalf("failed to parse reference %q: %v", tt.input, err)
			}

			got := CanMapToECRPublic(ref)
			if got != tt.expected {
				t.Errorf("CanMapToECRPublic(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMapToECRPublic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"alpine with tag", "alpine:3.18", "public.ecr.aws/docker/library/alpine:3.18"},
		{"alpine no tag", "alpine", "public.ecr.aws/docker/library/alpine"},
		{"explicit library", "docker.io/library/alpine:3.18", "public.ecr.aws/docker/library/alpine:3.18"},
		{"node image", "node:20", "public.ecr.aws/docker/library/node:20"},
		{"golang alpine", "golang:1.21-alpine", "public.ecr.aws/docker/library/golang:1.21-alpine"},
		{"python slim", "python:3.12-slim", "public.ecr.aws/docker/library/python:3.12-slim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.input)
			if err != nil {
				t.Fatalf("failed to parse reference %q: %v", tt.input, err)
			}

			got, err := MapToECRPublic(ref)
			if err != nil {
				t.Fatalf("MapToECRPublic(%q) returned error: %v", tt.input, err)
			}
			if got == nil {
				t.Fatalf("MapToECRPublic(%q) returned nil", tt.input)
			}

			gotStr := got.String()
			if gotStr != tt.expected {
				t.Errorf("MapToECRPublic(%q) = %q, want %q", tt.input, gotStr, tt.expected)
			}
		})
	}
}

func TestMapToECRPublic_NotEligible(t *testing.T) {
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

			got, err := MapToECRPublic(ref)
			if !errors.Is(err, ErrNotEligible) {
				t.Errorf("MapToECRPublic(%q) error = %v, want ErrNotEligible", tt.input, err)
			}
			if got != nil {
				t.Errorf("MapToECRPublic(%q) = %q, want nil", tt.input, got.String())
			}
		})
	}
}
