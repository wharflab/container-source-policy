package dockerfile

import (
	"context"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/containers/image/v5/docker/reference"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// ImageRef represents a container image reference extracted from a Dockerfile
type ImageRef struct {
	// Original is the original image reference as it appears in the Dockerfile
	Original string
	// Ref is the parsed and normalized reference
	Ref reference.Named
	// Line is the line number in the Dockerfile where this reference appears
	Line int
	// StageName is the build stage name if this is a named stage
	StageName string
}

// HTTPSourceRef represents an HTTP/HTTPS source reference extracted from a Dockerfile ADD instruction.
// Note: ADD instructions with --checksum flag are excluded (already pinned).
type HTTPSourceRef struct {
	// URL is the HTTP/HTTPS URL as it appears in the Dockerfile
	URL string
	// Line is the line number in the Dockerfile where this reference appears
	Line int
}

// GitSourceRef represents a Git source reference extracted from a Dockerfile ADD instruction.
// BuildKit supports git URLs in ADD instructions for fetching repositories during build.
type GitSourceRef struct {
	// URL is the Git URL as it appears in the Dockerfile (e.g., https://github.com/owner/repo.git#ref)
	URL string
	// Line is the line number in the Dockerfile where this reference appears
	Line int
}

// ParseResult contains all extracted references from a Dockerfile
type ParseResult struct {
	// Images contains all container image references (FROM and COPY --from instructions)
	Images []ImageRef
	// HTTPSources contains all HTTP/HTTPS source references (ADD instructions without checksum)
	HTTPSources []HTTPSourceRef
	// GitSources contains all Git source references (ADD instructions)
	GitSources []GitSourceRef
}

// openDockerfile opens a Dockerfile path for reading.
// If path is "-", returns os.Stdin and a no-op closer.
// Otherwise, opens the file and returns it with its Close method.
func openDockerfile(path string) (io.Reader, func() error, error) {
	if path == "-" {
		return os.Stdin, func() error { return nil }, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

// ParseFile parses a Dockerfile and extracts all image references
func ParseFile(ctx context.Context, path string) ([]ImageRef, error) {
	r, closer, err := openDockerfile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	return Parse(ctx, r)
}

// Parse parses a Dockerfile from a reader and extracts all image references
func Parse(ctx context.Context, r io.Reader) ([]ImageRef, error) {
	result, err := ParseAll(ctx, r)
	if err != nil {
		return nil, err
	}
	return result.Images, nil
}

// ParseAllFile parses a Dockerfile and extracts all references (images and HTTP sources)
func ParseAllFile(ctx context.Context, path string) (*ParseResult, error) {
	r, closer, err := openDockerfile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()
	return ParseAll(ctx, r)
}

// ParseAll parses a Dockerfile from a reader and extracts all references
func ParseAll(ctx context.Context, r io.Reader) (*ParseResult, error) {
	ast, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}

	// Use BuildKit's higher-level instruction parser
	stages, _, err := instructions.Parse(ast.AST, nil)
	if err != nil {
		return nil, err
	}

	parseResult := &ParseResult{
		Images:      []ImageRef{},
		HTTPSources: []HTTPSourceRef{},
		GitSources:  []GitSourceRef{},
	}

	// Track stage names for detecting multi-stage references
	stageNames := make(map[string]bool)

	for _, stage := range stages {
		// Extract image reference from stage
		if ref := extractImageRef(stage, stageNames); ref != nil {
			parseResult.Images = append(parseResult.Images, *ref)
		}

		// Track stage name for subsequent stages
		if stage.Name != "" {
			stageNames[strings.ToLower(stage.Name)] = true
		}

		// Extract image references and sources from commands in this stage
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.AddCommand:
				httpRefs, gitRefs := extractAddSources(c)
				parseResult.HTTPSources = append(parseResult.HTTPSources, httpRefs...)
				parseResult.GitSources = append(parseResult.GitSources, gitRefs...)
			case *instructions.CopyCommand:
				if ref := extractCopyFromImage(c, stageNames); ref != nil {
					parseResult.Images = append(parseResult.Images, *ref)
				}
			}
		}
	}

	return parseResult, nil
}

// parseImageReference validates and parses an image reference string.
// Returns nil if the reference should be skipped (scratch, stage reference, variable, already pinned, or invalid).
func parseImageReference(imageName string, line int, stageNames map[string]bool) *ImageRef {
	// Skip scratch base image
	if strings.EqualFold(imageName, "scratch") {
		return nil
	}

	// Skip numeric stage indices (COPY --from=0, COPY --from=1, etc.)
	if isNumeric(imageName) {
		return nil
	}

	// Skip references to previous build stages (multi-stage builds)
	if stageNames[strings.ToLower(imageName)] {
		return nil
	}

	// Skip references containing unexpanded ARG/ENV variables
	if containsVariable(imageName) {
		return nil
	}

	// Skip images already pinned by digest (e.g., name@sha256:...)
	if strings.Contains(imageName, "@sha256:") {
		return nil
	}

	// Parse the image reference using containers/image library
	named, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		// Return nil instead of error - invalid refs are skipped
		return nil
	}

	return &ImageRef{
		Original: imageName,
		Ref:      named,
		Line:     line,
	}
}

// extractImageRef extracts an image reference from a stage's FROM instruction
func extractImageRef(stage instructions.Stage, stageNames map[string]bool) *ImageRef {
	line := 0
	if len(stage.Location) > 0 {
		line = stage.Location[0].Start.Line
	}

	ref := parseImageReference(stage.BaseName, line, stageNames)
	if ref != nil {
		ref.StageName = stage.Name
	}
	return ref
}

// extractCopyFromImage extracts an image reference from a COPY --from instruction
func extractCopyFromImage(copyCmd *instructions.CopyCommand, stageNames map[string]bool) *ImageRef {
	// No --from flag or empty value
	if copyCmd.From == "" {
		return nil
	}

	line := 0
	if locs := copyCmd.Location(); len(locs) > 0 {
		line = locs[0].Start.Line
	}

	return parseImageReference(copyCmd.From, line, stageNames)
}

// extractAddSources extracts HTTP/HTTPS and Git URLs from an ADD command
func extractAddSources(addCmd *instructions.AddCommand) ([]HTTPSourceRef, []GitSourceRef) {
	// If checksum is already specified, skip this ADD
	if addCmd.Checksum != "" {
		return nil, nil
	}

	var httpRefs []HTTPSourceRef
	var gitRefs []GitSourceRef
	line := 0
	if locs := addCmd.Location(); len(locs) > 0 {
		line = locs[0].Start.Line
	}

	for _, src := range addCmd.SourcePaths {
		// Skip sources containing unexpanded variables
		if containsVariable(src) {
			continue
		}

		if isGitURL(src) {
			// Git URLs
			gitRefs = append(gitRefs, GitSourceRef{
				URL:  src,
				Line: line,
			})
		} else if isHTTPURL(src) {
			// HTTP/HTTPS URLs (non-git)
			httpRefs = append(httpRefs, HTTPSourceRef{
				URL:  src,
				Line: line,
			})
		}
	}

	return httpRefs, gitRefs
}

// containsVariable checks if the string contains unexpanded ARG/ENV syntax
// Detects ${VAR}, $VAR patterns
func containsVariable(s string) bool {
	if strings.Contains(s, "${") {
		return true
	}
	// Check for $VAR pattern (variable without braces)
	for i := range len(s) {
		if s[i] == '$' && i+1 < len(s) {
			next := s[i+1]
			// $VAR pattern: $ followed by letter or underscore
			if (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || next == '_' {
				return true
			}
		}
	}
	return false
}

// isGitURL checks if a string is a Git URL
// Git URLs can be:
// - URLs ending with .git (https://github.com/owner/repo.git)
// - git:// protocol URLs
// - ssh:// protocol URLs with git@
// - git@ prefix (git@github.com:owner/repo)
func isGitURL(s string) bool {
	// Check for git@ prefix (SSH format without scheme)
	if strings.HasPrefix(s, "git@") {
		return true
	}

	// Try parsing as URL
	u, err := url.Parse(s)
	if err != nil {
		return false
	}

	scheme := strings.ToLower(u.Scheme)

	// Check git:// or ssh:// protocols
	if scheme == "git" || scheme == "ssh" {
		return true
	}

	// Check for .git suffix on http/https URLs
	if (scheme == "http" || scheme == "https") && strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), ".git") {
		return true
	}

	return false
}

// isHTTPURL checks if a string is an HTTP or HTTPS URL (non-git)
func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

// isNumeric checks if a string is a non-negative integer (for stage indices like "0", "1", etc.)
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
