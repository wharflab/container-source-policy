package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

var binaryPath string

func TestMain(m *testing.M) {
	// Build the binary once before running tests
	tmpDir, err := os.MkdirTemp("", "container-source-policy-test")
	if err != nil {
		panic(err)
	}

	binaryPath = filepath.Join(tmpDir, "container-source-policy")

	// Build the module's main package
	cmd := exec.Command("go", "build", "-o", binaryPath, "github.com/tinovyatkin/container-source-policy")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("failed to build binary: " + string(out))
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func TestPinDryRun(t *testing.T) {
	testCases := []struct {
		name string
		dir  string
	}{
		{"simple", "simple"},
		{"multistage", "multistage"},
		{"ghcr", "ghcr"},
		{"scratch", "scratch"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dockerfilePath := filepath.Join("testdata", tc.dir, "Dockerfile")

			cmd := exec.Command(binaryPath, "pin", "--dry-run", "--stdout", dockerfilePath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command failed: %v\noutput: %s", err, output)
			}

			snaps.MatchSnapshot(t, string(output))
		})
	}
}
