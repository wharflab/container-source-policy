package cmd

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/container-source-policy/internal/version"
)

// NewApp creates the CLI application
func NewApp() *cli.Command {
	return &cli.Command{
		Name:    "container-source-policy",
		Usage:   "CLI for generating Docker BuildKit source policy files",
		Version: version.Version(),
		Description: `container-source-policy is a CLI utility for generating and managing
Docker container source policy files.

It parses Dockerfiles to extract image references and generates policy files
that can pin images to specific digests for reproducible builds.

Usage with docker buildx:
  EXPERIMENTAL_BUILDKIT_SOURCE_POLICY=policy.json docker buildx build .

Usage with buildctl:
  buildctl build --source-policy-file policy.json ...`,
		Commands: []*cli.Command{
			pinCommand(),
			versionCommand(),
		},
	}
}

// Execute runs the CLI application
func Execute() error {
	return NewApp().Run(context.Background(), os.Args)
}
