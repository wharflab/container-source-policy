// Package mcr provides utilities for mapping container image references to
// Microsoft Container Registry (mcr.microsoft.com/mirror/docker/library/*) equivalents.
package mcr

import (
	"errors"
	"strings"

	"github.com/containers/image/v5/docker/reference"
)

// ErrNotEligible is returned when an image reference cannot be mapped to MCR.
var ErrNotEligible = errors.New("image not eligible for MCR mapping")

const (
	// Registry is the MCR hostname
	Registry = "mcr.microsoft.com"
	// DockerHubDomain is the canonical Docker Hub domain
	DockerHubDomain = "docker.io"
	// LibraryPrefix is the path prefix for official Docker Hub images
	LibraryPrefix = "library/"
	// MirrorPrefix is the MCR path prefix for Docker Hub library mirrors
	MirrorPrefix = "mirror/docker/"
)

// CanMapToMCR returns true if the reference is a docker.io library image
// that may have an MCR mirror equivalent.
// Only official images (docker.io/library/*) are eligible for MCR mirror mapping.
func CanMapToMCR(ref reference.Named) bool {
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

// MapToMCR converts a docker.io library reference to its MCR mirror equivalent.
// Example: docker.io/library/alpine:3.18 -> mcr.microsoft.com/mirror/docker/library/alpine:3.18
// Returns ErrNotEligible if the reference cannot be mapped (use CanMapToMCR to check first).
func MapToMCR(ref reference.Named) (reference.Named, error) {
	if !CanMapToMCR(ref) {
		return nil, ErrNotEligible
	}

	path := reference.Path(ref)
	// Prepend "mirror/docker/" to the path: library/alpine -> mirror/docker/library/alpine
	mcrPath := MirrorPrefix + path

	// Build mcr.microsoft.com reference string
	mcrRefStr := Registry + "/" + mcrPath

	// Preserve tag if present
	if tagged, ok := ref.(reference.Tagged); ok {
		mcrRefStr += ":" + tagged.Tag()
	}

	// Preserve digest if present
	if digested, ok := ref.(reference.Digested); ok {
		mcrRefStr += "@" + digested.Digest().String()
	}

	return reference.ParseNormalizedNamed(mcrRefStr)
}
