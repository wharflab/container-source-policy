// Package ecrpublic provides utilities for mapping container image references
// to AWS ECR Public Gallery (public.ecr.aws/docker/library/*) equivalents.
package ecrpublic

import (
	"errors"
	"strings"

	"github.com/containers/image/v5/docker/reference"
)

// ErrNotEligible is returned when an image reference cannot be mapped to ECR Public.
var ErrNotEligible = errors.New("image not eligible for ECR Public mapping")

const (
	// Registry is the ECR Public Gallery hostname
	Registry = "public.ecr.aws"
	// DockerHubDomain is the canonical Docker Hub domain
	DockerHubDomain = "docker.io"
	// LibraryPrefix is the path prefix for official Docker Hub images
	LibraryPrefix = "library/"
	// ECRDockerPrefix is the ECR Public path prefix for Docker official images
	ECRDockerPrefix = "docker/"
)

// CanMapToECRPublic returns true if the reference is a docker.io library image
// that may have an ECR Public Gallery equivalent.
// Only official images (docker.io/library/*) are eligible for ECR Public mapping.
func CanMapToECRPublic(ref reference.Named) bool {
	domain := reference.Domain(ref)
	path := reference.Path(ref)

	// Only docker.io images
	if domain != DockerHubDomain {
		return false
	}

	// Only library images (docker.io/library/*)
	// These are the official images like alpine, node, golang, etc.
	return strings.HasPrefix(path, LibraryPrefix)
}

// MapToECRPublic converts a docker.io library reference to its ECR Public Gallery equivalent.
// Example: docker.io/library/alpine:3.18 -> public.ecr.aws/docker/library/alpine:3.18
// Returns ErrNotEligible if the reference cannot be mapped (use CanMapToECRPublic to check first).
func MapToECRPublic(ref reference.Named) (reference.Named, error) {
	if !CanMapToECRPublic(ref) {
		return nil, ErrNotEligible
	}

	path := reference.Path(ref)
	// Prepend "docker/" to the path: library/alpine -> docker/library/alpine
	ecrPath := ECRDockerPrefix + path

	// Build public.ecr.aws reference string
	ecrRefStr := Registry + "/" + ecrPath

	// Preserve tag if present
	if tagged, ok := ref.(reference.Tagged); ok {
		ecrRefStr += ":" + tagged.Tag()
	}

	return reference.ParseNormalizedNamed(ecrRefStr)
}
