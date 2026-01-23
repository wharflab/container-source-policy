package pin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/containers/image/v5/docker/reference"
	"github.com/opencontainers/go-digest"

	"github.com/tinovyatkin/container-source-policy/internal/dockerfile"
	"github.com/tinovyatkin/container-source-policy/internal/git"
	httpclient "github.com/tinovyatkin/container-source-policy/internal/http"
	"github.com/tinovyatkin/container-source-policy/internal/policy"
	"github.com/tinovyatkin/container-source-policy/internal/registry"
)

// Options configures the pin operation
type Options struct {
	Dockerfiles []string
}

// GeneratePolicy parses Dockerfiles and generates a source policy with pinned digests
func GeneratePolicy(ctx context.Context, opts Options) (*policy.Policy, error) {
	registryClient := registry.NewClient()
	httpClient := httpclient.NewClient()
	gitClient := git.NewClient()
	pol := policy.NewPolicy()

	seenImages := make(map[string]bool)
	seenHTTP := make(map[string]bool)
	seenGit := make(map[string]bool)

	for _, dockerfilePath := range opts.Dockerfiles {
		parseResult, err := dockerfile.ParseAllFile(ctx, dockerfilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", dockerfilePath, err)
		}

		// Process image references (FROM instructions)
		for _, ref := range parseResult.Images {
			// Skip if already processed
			if seenImages[ref.Original] {
				continue
			}
			seenImages[ref.Original] = true

			// Skip references that already have a digest
			if _, ok := ref.Ref.(reference.Digested); ok {
				continue
			}

			// Get the digest from the registry
			digestStr, err := registryClient.GetDigest(ctx, ref.Ref)
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
			policy.AddPinRule(pol, ref.Original, pinnedRef.String())
		}

		// Process HTTP sources (ADD instructions without checksum)
		for _, httpRef := range parseResult.HTTPSources {
			// Skip if already processed
			if seenHTTP[httpRef.URL] {
				continue
			}
			seenHTTP[httpRef.URL] = true

			// Get the checksum for the HTTP resource
			checksum, err := httpClient.GetChecksum(ctx, httpRef.URL)
			if err != nil {
				// Skip resources that require authentication instead of failing
				if httpclient.IsAuthError(err) {
					log.Printf("Warning: Skipping %s (authentication required)", httpRef.URL)
					continue
				}
				return nil, fmt.Errorf("failed to get checksum for %s: %w", httpRef.URL, err)
			}

			// Add the HTTP checksum rule
			policy.AddHTTPChecksumRule(pol, httpRef.URL, checksum)
		}

		// Process Git sources (ADD instructions with git URLs)
		for _, gitRef := range parseResult.GitSources {
			// Skip if already processed
			if seenGit[gitRef.URL] {
				continue
			}
			seenGit[gitRef.URL] = true

			// Get the commit checksum for the Git repository
			checksum, err := gitClient.GetCommitChecksum(ctx, gitRef.URL)
			if err != nil {
				return nil, fmt.Errorf("failed to get commit checksum for %s: %w", gitRef.URL, err)
			}

			// Add the Git checksum rule
			policy.AddGitChecksumRule(pol, gitRef.URL, checksum)
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
