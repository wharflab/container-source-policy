// Package git provides functionality to resolve Git repository commit checksums
package git

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moby/buildkit/util/gitutil"
)

// Client handles git operations
type Client struct{}

// NewClient creates a new git client
func NewClient() *Client {
	return &Client{}
}

// GitRef represents a parsed git reference
type GitRef struct {
	// Remote is the git remote URL (without fragment)
	Remote string
	// Ref is the git reference (branch, tag, or commit)
	Ref string
	// Subdir is the optional subdirectory path
	Subdir string
}

// ParseGitURL parses a git URL that may contain a fragment with ref and subdir
// Format: <url>#<ref>[:<subdir>]
// Examples:
//   - https://github.com/owner/repo.git#v1.0.0
//   - https://github.com/owner/repo.git#main:subdirectory
//   - git@github.com:owner/repo.git#branch
func ParseGitURL(rawURL string) (*GitRef, error) {
	// Split on # to separate URL from fragment
	parts := strings.SplitN(rawURL, "#", 2)
	remote := parts[0]

	ref := "HEAD" // default ref
	subdir := ""

	if len(parts) == 2 {
		// Parse fragment: ref[:subdir]
		fragment := parts[1]
		if fragment != "" {
			refParts := strings.SplitN(fragment, ":", 2)
			ref = refParts[0]
			if ref == "" {
				ref = "HEAD"
			}
			if len(refParts) == 2 {
				subdir = refParts[1]
			}
		}
	}

	return &GitRef{
		Remote: remote,
		Ref:    ref,
		Subdir: subdir,
	}, nil
}

// GetCommitChecksum resolves a git reference to its commit SHA
// Uses git ls-remote to fetch the commit without cloning the repository
// Delegates to BuildKit's gitutil for authentication, SSH, and proxy support
func (c *Client) GetCommitChecksum(ctx context.Context, rawURL string) (string, error) {
	gitRef, err := ParseGitURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse git URL: %w", err)
	}

	// Apply default timeout if context has no deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Use BuildKit's GitCLI which handles:
	// - SSH authentication and known_hosts
	// - HTTP proxy environment variables
	// - Auth tokens and headers
	// - Non-interactive mode (GIT_TERMINAL_PROMPT=0)
	// - Consistent output formatting
	git := gitutil.NewGitCLI()

	// Request both the ref and its dereferenced form (for annotated tags)
	// BuildKit's approach: source/git/source.go:264
	output, err := git.Run(ctx, "ls-remote", gitRef.Remote, gitRef.Ref, gitRef.Ref+"^{}")
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed: %w", err)
	}

	// Parse output: <commit-sha>\t<ref-name>
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("no commit found for ref %s", gitRef.Ref)
	}

	// Prefer annotated-tag deref (^{}) if present; otherwise use first entry.
	// For annotated tags, git ls-remote returns both the tag object and the commit:
	//   abc123def  refs/tags/v1.0.0
	//   54d56cab   refs/tags/v1.0.0^{}  ← actual commit (preferred)
	commitSHA := ""
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sha, refName := fields[0], fields[1]
		if strings.HasSuffix(refName, "^{}") {
			commitSHA = sha
			break
		}
		if commitSHA == "" {
			commitSHA = sha
		}
	}
	if commitSHA == "" {
		return "", errors.New("unexpected git ls-remote output format")
	}
	if len(commitSHA) != 40 {
		return "", fmt.Errorf("invalid commit SHA length: %d", len(commitSHA))
	}
	if _, err := hex.DecodeString(commitSHA); err != nil {
		return "", errors.New("invalid commit SHA format: not hexadecimal")
	}

	return commitSHA, nil
}
