package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinovyatkin/container-source-policy/internal/pin"
)

var (
	outputFile string
	useStdout  bool
	dryRun     bool
)

var pinCmd = &cobra.Command{
	Use:   "pin [DOCKERFILE...]",
	Short: "Generate a source policy file with pinned image digests",
	Long: `Parse Dockerfile(s) to extract image references (FROM instructions)
and generate a BuildKit source policy file that pins each image to its
current digest.

Example:
  container-source-policy pin --output policy.json Dockerfile
  container-source-policy pin --stdout Dockerfile.* > policy.json
  cat Dockerfile | container-source-policy pin --stdout -`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPin,
}

func init() {
	pinCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path for the policy JSON")
	pinCmd.Flags().BoolVar(&useStdout, "stdout", false, "Write policy to stdout instead of file")
	pinCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse Dockerfiles and show what would be pinned without fetching digests")
	pinCmd.MarkFlagsMutuallyExclusive("output", "stdout")
}

func runPin(cmd *cobra.Command, args []string) error {
	opts := pin.Options{
		Dockerfiles: args,
		DryRun:      dryRun,
	}

	policy, err := pin.GeneratePolicy(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("failed to generate policy: %w", err)
	}

	var w io.Writer
	if useStdout || outputFile == "" {
		w = os.Stdout
	} else {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	if err := pin.WritePolicy(w, policy); err != nil {
		return fmt.Errorf("failed to write policy: %w", err)
	}

	return nil
}
