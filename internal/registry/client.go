package registry

import (
	"context"
	"fmt"
	"os"

	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/pkg/cli/environment"
	"github.com/containers/image/v5/types"
)

// Client provides methods for interacting with container registries
type Client struct {
	sysCtx *types.SystemContext
}

// NewClient creates a new registry client
// It respects CONTAINERS_REGISTRIES_CONF environment variable for registry configuration
func NewClient() *Client {
	sysCtx := &types.SystemContext{}

	// Apply CONTAINERS_REGISTRIES_CONF or REGISTRIES_CONFIG_PATH env vars if set
	if err := environment.UpdateRegistriesConf(sysCtx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load registries config: %v\n", err)
	}

	return &Client{
		sysCtx: sysCtx,
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
	defer func() { _ = imgSrc.Close() }()

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
