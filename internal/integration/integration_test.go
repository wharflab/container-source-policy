package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestPinHTTPSources tests HTTP source policy generation with a mock HTTP server
func TestPinHTTPSources(t *testing.T) {
	// Create mock HTTP server
	mockHTTP := testutil.NewMockHTTPServer()
	defer mockHTTP.Close()

	// Add a test file to the mock server with deterministic content
	testContent := "This is test content for HTTP source policy testing.\n"
	expectedChecksum := mockHTTP.AddFile("/test/file.txt", testContent)

	// Create a temporary Dockerfile that uses the mock server URL
	tmpDir, err := os.MkdirTemp("", "http-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	dockerfileContent := `FROM alpine:3.18
ADD ` + mockHTTP.URL() + `/test/file.txt /app/file.txt
`
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
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

	// Verify mock HTTP server was hit
	if !mockHTTP.HasRequest("/test/file.txt") {
		t.Errorf("expected mock HTTP server to be hit for /test/file.txt, but it wasn't.\nRequests: %v", mockHTTP.Requests())
	}

	// Parse the output and verify it contains the HTTP checksum rule
	var pol policy.Policy
	if err := json.Unmarshal(output, &pol); err != nil {
		t.Fatalf("failed to parse policy output: %v\noutput: %s", err, output)
	}

	// Should have 2 rules: one for alpine:3.18, one for HTTP source
	if len(pol.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(pol.Rules))
	}

	// Find the HTTP rule
	var httpRule *policy.Rule
	for i := range pol.Rules {
		if strings.Contains(pol.Rules[i].Selector.Identifier, mockHTTP.URL()) {
			httpRule = &pol.Rules[i]
			break
		}
	}

	if httpRule == nil {
		t.Fatal("HTTP rule not found in policy output")
	}

	// Verify the HTTP rule
	if httpRule.Action != policy.PolicyActionConvert {
		t.Errorf("expected action CONVERT, got %s", httpRule.Action)
	}

	if httpRule.Updates == nil || httpRule.Updates.Attrs == nil {
		t.Fatal("HTTP rule missing updates.attrs")
	}

	if httpRule.Updates.Attrs["http.checksum"] != expectedChecksum {
		t.Errorf("expected checksum %s, got %s", expectedChecksum, httpRule.Updates.Attrs["http.checksum"])
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
	tmpDir, err := os.MkdirTemp("", "http-test-checksum")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	dockerfileContent := `FROM alpine:3.18
ADD --checksum=sha256:0000000000000000000000000000000000000000000000000000000000000000 ` + mockHTTP.URL() + `/test/file.txt /app/file.txt
`
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
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
		t.Errorf("expected mock HTTP server NOT to be hit (ADD has --checksum), but it was.\nRequests: %v", mockHTTP.Requests())
	}

	// Parse the output - should only have the alpine:3.18 rule, no HTTP rule
	var pol policy.Policy
	if err := json.Unmarshal(output, &pol); err != nil {
		t.Fatalf("failed to parse policy output: %v\noutput: %s", err, output)
	}

	// Should have only 1 rule (for alpine:3.18)
	if len(pol.Rules) != 1 {
		t.Errorf("expected 1 rule (no HTTP rule since --checksum present), got %d", len(pol.Rules))
	}
}
