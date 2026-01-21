package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "container-source-policy",
	Short: "CLI for generating Docker BuildKit source policy files",
	Long: `container-source-policy is a CLI utility for generating and managing
Docker container source policy files (BuildKit --source-policy-file).

It parses Dockerfiles to extract image references and generates policy files
that can pin images to specific digests for reproducible builds.`,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(pinCmd)
	rootCmd.AddCommand(versionCmd)
}
