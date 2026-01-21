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
		defer f.Close()
		r = f
	}
	return Parse(ctx, r)
}

// Parse parses a Dockerfile from a reader and extracts all image references
func Parse(ctx context.Context, r io.Reader) ([]ImageRef, error) {
	result, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}

	var refs []ImageRef
	for _, child := range result.AST.Children {
		if strings.EqualFold(child.Value, "from") {
			ref, err := parseFromInstruction(child)
			if err != nil {
				return nil, err
			}
			if ref != nil {
				refs = append(refs, *ref)
			}
		}
	}

	return refs, nil
}

func parseFromInstruction(node *parser.Node) (*ImageRef, error) {
	if node.Next == nil {
		return nil, nil
	}

	original := node.Next.Value

	// Skip scratch base image
	if strings.EqualFold(original, "scratch") {
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
