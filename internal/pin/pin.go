package pin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/containers/image/v5/docker/reference"
	"github.com/opencontainers/go-digest"
	"github.com/tinovyatkin/container-source-policy/internal/dockerfile"
	"github.com/tinovyatkin/container-source-policy/internal/policy"
	"github.com/tinovyatkin/container-source-policy/internal/registry"
)

// Options configures the pin operation
type Options struct {
	Dockerfiles []string
	DryRun      bool
}

// GeneratePolicy parses Dockerfiles and generates a source policy with pinned digests
func GeneratePolicy(ctx context.Context, opts Options) (*policy.Policy, error) {
	var client *registry.Client
	if !opts.DryRun {
		client = registry.NewClient()
	}
	pol := policy.NewPolicy()

	seen := make(map[string]bool)

	for _, dockerfilePath := range opts.Dockerfiles {
		refs, err := dockerfile.ParseFile(ctx, dockerfilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", dockerfilePath, err)
		}

		for _, ref := range refs {
			// Skip if already processed
			if seen[ref.Original] {
				continue
			}
			seen[ref.Original] = true

			// Skip references that already have a digest
			if _, ok := ref.Ref.(reference.Digested); ok {
				continue
			}

			if opts.DryRun {
				// In dry-run mode, use a placeholder digest
				pol.AddPinRule(ref.Original, ref.Ref.String()+"@sha256:dry-run-placeholder")
				continue
			}

			// Get the digest from the registry
			digestStr, err := client.GetDigest(ctx, ref.Ref)
			if err != nil {
				return nil, fmt.Errorf("failed to get digest for %s: %w", ref.Original, err)
			}

			// Build the pinned reference
			d, err := digest.Parse(digestStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse digest %s: %w", digestStr, err)
			}

			pinnedRef, err := reference.WithDigest(ref.Ref, d)
			if err != nil {
				return nil, fmt.Errorf("failed to create pinned reference for %s: %w", ref.Original, err)
			}

			// Add the pin rule
			pol.AddPinRule(ref.Original, pinnedRef.String())
		}
	}

	return pol, nil
}

// WritePolicy writes a policy to the given writer as JSON
func WritePolicy(w io.Writer, pol *policy.Policy) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(pol)
}
