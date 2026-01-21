package main

import (
	"os"

	"github.com/tinovyatkin/container-source-policy/cmd/container-source-policy/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
