package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/tinovyatkin/container-source-policy/internal/policy"
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
		{"library/busybox", "1.36", 4},
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
		requiresNet    bool     // skip in short mode if true
	}{
		{"simple", "simple", []string{"library/alpine/manifests/3.18"}, false},
		{"multistage", "multistage", []string{"library/golang/manifests/1.21", "library/alpine/manifests/3.18"}, false},
		{"ghcr", "ghcr", []string{"actions/actions-runner/manifests/latest"}, false},
		{"scratch", "scratch", []string{"library/golang/manifests/1.21"}, false},
		{"copy-from", "copy-from", []string{"library/alpine/manifests/3.18", "library/busybox/manifests/1.36"}, false},
		{"http-add", "http-add", []string{"library/alpine/manifests/3.18"}, true}, // hits real GitHub URL
		{"git-add", "git-add", []string{"library/alpine/manifests/3.18"}, true},   // hits real GitHub git repo
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.requiresNet && testing.Short() {
				t.Skip("skipping test requiring network in short mode")
			}

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
					t.Errorf(
						"expected mock registry to be hit for %s, but it wasn't.\nRequests: %v",
						img,
						mockRegistry.Requests(),
					)
				}
			}

			// Verify no unexpected manifest requests were made
			manifestRequests := mockRegistry.RequestCount("/manifests/")
			if manifestRequests != len(tc.expectedImages) {
				t.Errorf(
					"expected %d manifest requests, got %d.\nRequests: %v",
					len(tc.expectedImages),
					manifestRequests,
					mockRegistry.Requests(),
				)
			}

			// Validate the policy using BuildKit's sourcepolicy types
			var pol policy.Policy
			if err := json.Unmarshal(output, &pol); err != nil {
				t.Fatalf("failed to parse policy output: %v", err)
			}
			if err := policy.Validate(&pol); err != nil {
				t.Fatalf("policy validation failed: %v", err)
			}
			// Deeper validation: run rules through BuildKit's sourcepolicy engine
			if err := policy.ValidateWithEvaluate(context.Background(), &pol); err != nil {
				t.Fatalf("policy engine evaluation failed: %v", err)
			}

			snaps.WithConfig(snaps.Ext(".json")).MatchStandaloneSnapshot(t, string(output))
		})
	}
}

// TestPinHTTPSourcesWithExistingChecksum tests that ADD with --checksum is skipped
func TestPinHTTPSourcesWithExistingChecksum(t *testing.T) {
	// Create mock HTTP server
	mockHTTP := testutil.NewMockHTTPServer()
	defer mockHTTP.Close()

	// Add a test file to the mock server
	mockHTTP.AddFile("/test/file.txt", "test content")

	// Create a temporary Dockerfile that uses --checksum flag (should be skipped)
	tmpDir := t.TempDir()

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	dockerfileContent := `FROM alpine:3.18
ADD --checksum=sha256:0000000000000000000000000000000000000000000000000000000000000000 ` + mockHTTP.URL() + `/test/file.txt /app/file.txt
`
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset request tracking
	mockRegistry.ResetRequests()
	mockHTTP.ResetRequests()

	// Run the pin command
	cmd := exec.Command(binaryPath, "pin", "--stdout", dockerfilePath)
	cmd.Env = append(os.Environ(), "CONTAINERS_REGISTRIES_CONF="+registryConf)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, output)
	}

	// Verify mock HTTP server was NOT hit (because --checksum is already specified)
	if mockHTTP.HasRequest("/test/file.txt") {
		t.Errorf(
			"expected mock HTTP server NOT to be hit (ADD has --checksum), but it was.\nRequests: %v",
			mockHTTP.Requests(),
		)
	}

	// Parse the output - should only have the alpine:3.18 rule, no HTTP rule
	var pol policy.Policy
	if err := json.Unmarshal(output, &pol); err != nil {
		t.Fatalf("failed to parse policy output: %v\noutput: %s", err, output)
	}

	// Validate the policy using BuildKit's sourcepolicy types
	if err := policy.Validate(&pol); err != nil {
		t.Fatalf("policy validation failed: %v", err)
	}
	// Deeper validation: run rules through BuildKit's sourcepolicy engine
	if err := policy.ValidateWithEvaluate(context.Background(), &pol); err != nil {
		t.Fatalf("policy engine evaluation failed: %v", err)
	}

	// Should have only 1 rule (for alpine:3.18)
	if len(pol.Rules) != 1 {
		t.Errorf("expected 1 rule (no HTTP rule since --checksum present), got %d", len(pol.Rules))
	}
}
