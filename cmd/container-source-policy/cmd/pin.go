package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/container-source-policy/internal/pin"
)

func pinCommand() *cli.Command {
	return &cli.Command{
		Name:      "pin",
		Usage:     "Generate a source policy file with pinned image digests",
		ArgsUsage: "[DOCKERFILE...]",
		Description: `Parse Dockerfile(s) to extract image references (FROM instructions)
and generate a BuildKit source policy file that pins each image to its
current digest.

Example:
  container-source-policy pin --output policy.json Dockerfile
  container-source-policy pin --stdout Dockerfile.* > policy.json
  cat Dockerfile | container-source-policy pin --stdout -`,
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{{
			Flags: [][]cli.Flag{
				{&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "Output file path for the policy JSON",
				}},
				{&cli.BoolFlag{
					Name:  "stdout",
					Usage: "Write policy to stdout instead of file",
				}},
			},
		}},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 1 {
				return errors.New("at least one Dockerfile path is required")
			}

			opts := pin.Options{
				Dockerfiles: cmd.Args().Slice(),
			}

			policy, err := pin.GeneratePolicy(ctx, opts)
			if err != nil {
				return fmt.Errorf("failed to generate policy: %w", err)
			}

			outputFile := cmd.String("output")
			useStdout := cmd.Bool("stdout")

			var w io.Writer
			if useStdout || outputFile == "" {
				w = os.Stdout
			} else {
				f, err := os.Create(outputFile)
				if err != nil {
					return fmt.Errorf("failed to create output file: %w", err)
				}
				defer func() { _ = f.Close() }()
				w = f
			}

			if err := pin.WritePolicy(w, policy); err != nil {
				return fmt.Errorf("failed to write policy: %w", err)
			}

			return nil
		},
	}
}
