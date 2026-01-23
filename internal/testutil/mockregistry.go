package testutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// MockRegistry is a test registry server that serves images with deterministic digests
type MockRegistry struct {
	Server   *httptest.Server
	requests []string // tracks all requests made to the registry
	mu       sync.Mutex
}

// NewMockRegistry creates a new mock registry server
func NewMockRegistry() *MockRegistry {
	mr := &MockRegistry{}

	// Wrap the registry handler to track requests
	registryHandler := registry.New()
	mr.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := r.Method + " " + r.URL.Path
		mr.mu.Lock()
		mr.requests = append(mr.requests, req)
		mr.mu.Unlock()
		registryHandler.ServeHTTP(w, r)
	}))

	return mr
}

// Close shuts down the mock registry server
func (mr *MockRegistry) Close() {
	mr.Server.Close()
}

// Requests returns all requests made to the registry since last reset
func (mr *MockRegistry) Requests() []string {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	result := make([]string, len(mr.requests))
	copy(result, mr.requests)
	return result
}

// ResetRequests clears the tracked requests
func (mr *MockRegistry) ResetRequests() {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.requests = nil
}

// HasRequest checks if a request matching the pattern was made
// Pattern can be a substring match (e.g., "GET /v2/library/alpine/manifests")
func (mr *MockRegistry) HasRequest(pattern string) bool {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	for _, req := range mr.requests {
		if strings.Contains(req, pattern) {
			return true
		}
	}
	return false
}

// RequestCount returns the number of requests matching the pattern
func (mr *MockRegistry) RequestCount(pattern string) int {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	count := 0
	for _, req := range mr.requests {
		if strings.Contains(req, pattern) {
			count++
		}
	}
	return count
}

// Host returns the host:port of the mock registry
func (mr *MockRegistry) Host() string {
	return mr.Server.Listener.Addr().String()
}

// AddImage adds an image to the mock registry with a deterministic digest
// The imageID is used to generate unique but deterministic content
func (mr *MockRegistry) AddImage(repo, tag string, imageID int64) (string, error) {
	// Create a deterministic image using empty.Image with a unique config
	// The config includes the imageID to make each image unique but reproducible
	config := v1.Config{
		Labels: map[string]string{
			"mock.image.id": strconv.FormatInt(imageID, 10),
			"mock.repo":     repo,
			"mock.tag":      tag,
		},
	}

	img, err := mutate.Config(empty.Image, config)
	if err != nil {
		return "", err
	}

	// Parse the reference
	ref, err := name.ParseReference(mr.Host() + "/" + repo + ":" + tag)
	if err != nil {
		return "", err
	}

	// Push the image to our mock registry
	if err := remote.Write(ref, img); err != nil {
		return "", err
	}

	// Get the digest
	digest, err := img.Digest()
	if err != nil {
		return "", err
	}

	return digest.String(), nil
}

// AddEmptyImage adds an empty (scratch-based) image to the mock registry
func (mr *MockRegistry) AddEmptyImage(repo, tag string) (string, error) {
	img := empty.Image

	ref, err := name.ParseReference(mr.Host() + "/" + repo + ":" + tag)
	if err != nil {
		return "", err
	}

	if err := remote.Write(ref, img); err != nil {
		return "", err
	}

	digest, err := img.Digest()
	if err != nil {
		return "", err
	}

	return digest.String(), nil
}

// AddImageWithConfig adds an image with specific config to make digests deterministic
func (mr *MockRegistry) AddImageWithConfig(repo, tag string, config v1.Config) (string, error) {
	img, err := mutate.Config(empty.Image, config)
	if err != nil {
		return "", err
	}

	ref, err := name.ParseReference(mr.Host() + "/" + repo + ":" + tag)
	if err != nil {
		return "", err
	}

	if err := remote.Write(ref, img); err != nil {
		return "", err
	}

	digest, err := img.Digest()
	if err != nil {
		return "", err
	}

	return digest.String(), nil
}

// WriteRegistriesConf creates a registries.conf file that redirects the specified
// registries to the mock registry server. Returns the path to the created file.
func (mr *MockRegistry) WriteRegistriesConf(dir string, registries ...string) (string, error) {
	if len(registries) == 0 {
		registries = []string{"docker.io", "ghcr.io", "gcr.io", "quay.io"}
	}

	confPath := filepath.Join(dir, "registries.conf")
	f, err := os.Create(confPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	// Write header
	if _, err := fmt.Fprintln(f, `# Test registries.conf - redirects all registries to mock server`); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(f); err != nil {
		return "", err
	}

	// For each registry, create a redirect rule
	for _, reg := range registries {
		if _, err := fmt.Fprintf(f, `[[registry]]
prefix = "%s"
location = "%s"
insecure = true

`, reg, mr.Host()); err != nil {
			return "", err
		}
	}

	return confPath, nil
}
