// Package git provides functionality to resolve Git repository commit checksums
package git

import (
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
		refParts := strings.SplitN(fragment, ":", 2)
		ref = refParts[0]
		if len(refParts) == 2 {
			subdir = refParts[1]
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

	// Use git ls-remote to get the commit SHA
	// This works for branches and tags without cloning
	cmd := exec.CommandContext(ctx, "git", "ls-remote", gitRef.Remote, gitRef.Ref)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git ls-remote failed: %s: %w", string(exitErr.Stderr), err)
		}
		return "", fmt.Errorf("git ls-remote failed: %w", err)
	}

	// Parse output: <commit-sha>\t<ref-name>
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("no commit found for ref %s", gitRef.Ref)
	}

	// Take the first line and extract the SHA
	fields := strings.Fields(lines[0])
	if len(fields) < 1 {
		return "", fmt.Errorf("unexpected git ls-remote output format")
	}

	commitSHA := fields[0]
	if len(commitSHA) != 40 {
		return "", fmt.Errorf("invalid commit SHA length: %d", len(commitSHA))
	}
	if _, err := hex.DecodeString(commitSHA); err != nil {
		return "", fmt.Errorf("invalid commit SHA format: not hexadecimal")
	}

	return commitSHA, nil
}
