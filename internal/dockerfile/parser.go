package dockerfile

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/containers/image/v5/docker/reference"
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

// HTTPSourceRef represents an HTTP/HTTPS source reference extracted from a Dockerfile ADD instruction
type HTTPSourceRef struct {
	// URL is the HTTP/HTTPS URL as it appears in the Dockerfile
	URL string
	// Line is the line number in the Dockerfile where this reference appears
	Line int
	// HasChecksum indicates whether the ADD instruction already has a --checksum flag
	HasChecksum bool
}

// ParseResult contains all extracted references from a Dockerfile
type ParseResult struct {
	// Images contains all container image references (FROM instructions)
	Images []ImageRef
	// HTTPSources contains all HTTP/HTTPS source references (ADD instructions without checksum)
	HTTPSources []HTTPSourceRef
}

// ParseFile parses a Dockerfile and extracts all image references
func ParseFile(ctx context.Context, path string) ([]ImageRef, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
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
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	return ParseAll(ctx, r)
}

// ParseAll parses a Dockerfile from a reader and extracts all references
func ParseAll(ctx context.Context, r io.Reader) (*ParseResult, error) {
	result, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}

	parseResult := &ParseResult{
		Images:      []ImageRef{},
		HTTPSources: []HTTPSourceRef{},
	}
	stageNames := make(map[string]bool)

	for _, child := range result.AST.Children {
		switch {
		case strings.EqualFold(child.Value, "from"):
			ref, err := parseFromInstruction(child, stageNames)
			if err != nil {
				return nil, err
			}
			if ref != nil {
				parseResult.Images = append(parseResult.Images, *ref)
				// Track the stage name for subsequent FROM instructions
				if ref.StageName != "" {
					stageNames[strings.ToLower(ref.StageName)] = true
				}
			}
		case strings.EqualFold(child.Value, "add"):
			httpRefs := parseAddInstruction(child)
			parseResult.HTTPSources = append(parseResult.HTTPSources, httpRefs...)
		}
	}

	return parseResult, nil
}

// containsVariable checks if the string contains unexpanded ARG/ENV syntax
// Detects ${VAR}, $VAR patterns (but not $() command substitution which isn't valid in FROM)
func containsVariable(s string) bool {
	if strings.Contains(s, "${") {
		return true
	}
	// Check for $VAR pattern (variable without braces)
	for i := 0; i < len(s); i++ {
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

func parseFromInstruction(node *parser.Node, stageNames map[string]bool) (*ImageRef, error) {
	if node.Next == nil {
		return nil, nil
	}

	original := node.Next.Value

	// Skip scratch base image
	if strings.EqualFold(original, "scratch") {
		return nil, nil
	}

	// Skip references to previous build stages (multi-stage builds)
	if stageNames[strings.ToLower(original)] {
		return nil, nil
	}

	// Skip references containing unexpanded ARG/ENV variables
	if containsVariable(original) {
		return nil, nil
	}

	// Parse the image reference using containers/image library
	named, err := reference.ParseNormalizedNamed(original)
	if err != nil {
		return nil, err
	}

	ref := &ImageRef{
		Original: original,
		Ref:      named,
		Line:     node.StartLine,
	}

	// Check for AS clause (named stage)
	for n := node.Next; n != nil; n = n.Next {
		if strings.EqualFold(n.Value, "as") && n.Next != nil {
			ref.StageName = n.Next.Value
			break
		}
	}

	return ref, nil
}

// isHTTPURL checks if a string is an HTTP or HTTPS URL
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// parseAddInstruction extracts HTTP/HTTPS URLs from an ADD instruction
func parseAddInstruction(node *parser.Node) []HTTPSourceRef {
	var refs []HTTPSourceRef
	hasChecksum := false

	// Check for --checksum flag in the instruction flags
	for _, flag := range node.Flags {
		if strings.HasPrefix(flag, "--checksum=") {
			hasChecksum = true
			break
		}
	}

	// If checksum is already specified, we don't need to pin this ADD
	if hasChecksum {
		return refs
	}

	// Collect all arguments (sources and destination)
	var args []string
	for n := node.Next; n != nil; n = n.Next {
		args = append(args, n.Value)
	}

	// The last argument is the destination, everything else is sources
	if len(args) < 2 {
		return refs
	}
	sources := args[:len(args)-1]

	// Extract HTTP/HTTPS URLs from sources
	for _, src := range sources {
		// Skip sources containing unexpanded variables
		if containsVariable(src) {
			continue
		}

		// Only include HTTP/HTTPS URLs
		if isHTTPURL(src) {
			refs = append(refs, HTTPSourceRef{
				URL:         src,
				Line:        node.StartLine,
				HasChecksum: false,
			})
		}
	}

	return refs
}
