package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/tinovyatkin/container-source-policy/internal/testutil"
)

var (
	binaryPath   string
	mockRegistry *testutil.MockRegistry
	registryConf string
)

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

	// Start mock registry
	mockRegistry = testutil.NewMockRegistry()

	// Add all images used in test Dockerfiles with deterministic seeds
	// Using fixed seeds ensures deterministic digests for snapshot testing
	images := []struct {
		repo string
		tag  string
		seed int64
	}{
		{"library/alpine", "3.18", 1},
		{"library/golang", "1.21", 2},
		{"actions/actions-runner", "latest", 3},
	}

	for _, img := range images {
		if _, err := mockRegistry.AddImage(img.repo, img.tag, img.seed); err != nil {
			mockRegistry.Close()
			_ = os.RemoveAll(tmpDir)
			panic("failed to add image " + img.repo + ":" + img.tag + ": " + err.Error())
		}
	}

	// Create registries.conf that redirects all registries to our mock
	registryConf, err = mockRegistry.WriteRegistriesConf(tmpDir)
	if err != nil {
		mockRegistry.Close()
		_ = os.RemoveAll(tmpDir)
		panic("failed to create registries.conf: " + err.Error())
	}

	code := m.Run()

	mockRegistry.Close()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func TestPin(t *testing.T) {
	testCases := []struct {
		name           string
		dir            string
		expectedImages []string // image paths that should be fetched from mock registry
	}{
		{"simple", "simple", []string{"library/alpine/manifests/3.18"}},
		{"multistage", "multistage", []string{"library/golang/manifests/1.21", "library/alpine/manifests/3.18"}},
		{"ghcr", "ghcr", []string{"actions/actions-runner/manifests/latest"}},
		{"scratch", "scratch", []string{"library/golang/manifests/1.21"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset request tracking before each test
			mockRegistry.ResetRequests()

			dockerfilePath := filepath.Join("testdata", tc.dir, "Dockerfile")

			cmd := exec.Command(binaryPath, "pin", "--stdout", dockerfilePath)
			// Set CONTAINERS_REGISTRIES_CONF to use our mock registry
			cmd.Env = append(os.Environ(), "CONTAINERS_REGISTRIES_CONF="+registryConf)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command failed: %v\noutput: %s", err, output)
			}

			// Verify mock registry was hit for expected images
			for _, img := range tc.expectedImages {
				if !mockRegistry.HasRequest(img) {
					t.Errorf("expected mock registry to be hit for %s, but it wasn't.\nRequests: %v", img, mockRegistry.Requests())
				}
			}

			// Verify no unexpected manifest requests were made
			manifestRequests := mockRegistry.RequestCount("/manifests/")
			if manifestRequests != len(tc.expectedImages) {
				t.Errorf("expected %d manifest requests, got %d.\nRequests: %v", len(tc.expectedImages), manifestRequests, mockRegistry.Requests())
			}

			snaps.MatchSnapshot(t, string(output))
		})
	}
}
