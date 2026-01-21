package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tinovyatkin/container-source-policy/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("container-source-policy %s\n", version.Version())
	},
}
