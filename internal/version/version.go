package version

import (
	"fmt"
	"runtime/debug"
)

var (
	version = "dev"
	commit  = "unknown"
)

// Version returns the current version string
func Version() string {
	if commit != "unknown" && len(commit) > 7 {
		return version + " (" + commit[:7] + ")"
	}
	return version
}

// Commit returns the git commit hash
func Commit() string {
	return commit
}

// UserAgent returns a User-Agent string for HTTP requests
// Format: "container-source-policy/{version} buildkit/{buildkit-version}"
// Includes BuildKit version for servers that check compatibility
func UserAgent() string {
	buildkitVersion := "unknown"

	// Extract BuildKit version from build info
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/moby/buildkit" {
				buildkitVersion = dep.Version
				break
			}
		}
	}

	return fmt.Sprintf("container-source-policy/%s buildkit/%s", version, buildkitVersion)
}
