package registry

import (
	"context"
	"fmt"

	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/types"
)

// Client provides methods for interacting with container registries
type Client struct {
	sysCtx *types.SystemContext
}

// NewClient creates a new registry client
func NewClient() *Client {
	return &Client{
		sysCtx: &types.SystemContext{},
	}
}

// GetDigest resolves an image reference to its digest
func (c *Client) GetDigest(ctx context.Context, ref reference.Named) (string, error) {
	// Add default tag if not present
	if _, ok := ref.(reference.Tagged); !ok {
		if _, ok := ref.(reference.Digested); !ok {
			var err error
			ref, err = reference.WithTag(ref, "latest")
			if err != nil {
				return "", fmt.Errorf("failed to add default tag: %w", err)
			}
		}
	}

	imgRef, err := docker.NewReference(ref)
	if err != nil {
		return "", fmt.Errorf("failed to create docker reference: %w", err)
	}

	imgSrc, err := imgRef.NewImageSource(ctx, c.sysCtx)
	if err != nil {
		return "", fmt.Errorf("failed to create image source for %s: %w", ref.String(), err)
	}
	defer imgSrc.Close()

	manifestBytes, _, err := imgSrc.GetManifest(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest for %s: %w", ref.String(), err)
	}

	digest, err := manifest.Digest(manifestBytes)
	if err != nil {
		return "", fmt.Errorf("failed to compute manifest digest for %s: %w", ref.String(), err)
	}

	return digest.String(), nil
}
