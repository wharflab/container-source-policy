# CLAUDE.md - Project Guidance

## Project Overview

`container-source-policy` is a CLI tool for generating BuildKit source policy files (`--source-policy-file` input for `docker buildx build`). It parses Dockerfiles to extract image references and pins them to their current digests.

## Design Philosophy

**Minimize code ownership** - This project heavily reuses existing, well-maintained libraries:
- `github.com/moby/buildkit/frontend/dockerfile/parser` - Official Dockerfile parsing
- `github.com/containers/image/v5/docker/reference` - Image reference parsing and normalization
- `github.com/containers/image/v5/docker` - Registry interaction
- `github.com/containers/image/v5/manifest` - Manifest digest computation
- `github.com/spf13/cobra` - CLI framework

Do not re-implement functionality that exists in these libraries.

## Build & Test Commands

```bash
# Build
go build ./...

# Run all tests
go test ./...

# Run tests with verbose output
go test ./... -v

# Update snapshots for integration tests
UPDATE_SNAPS=true go test ./internal/integration/...

# Run the CLI
go run . pin --help
go run . pin --stdout Dockerfile
```

## Project Structure

```
.
├── main.go                           # Entry point
├── cmd/container-source-policy/cmd/  # CLI commands (cobra)
│   ├── root.go                       # Root command setup
│   ├── pin.go                        # Pin subcommand
│   └── version.go                    # Version subcommand
├── internal/
│   ├── dockerfile/                   # Dockerfile parsing (uses buildkit)
│   │   ├── parser.go
│   │   └── parser_test.go
│   ├── registry/                     # Registry client (uses containers/image)
│   │   └── client.go
│   ├── policy/                       # BuildKit source policy types
│   │   └── types.go
│   ├── pin/                          # Pin operation logic
│   │   └── pin.go
│   ├── integration/                  # Integration tests (go-snaps)
│   │   ├── integration_test.go
│   │   ├── __snapshots__/
│   │   └── testdata/                 # Test fixtures (each in own directory)
│   ├── testutil/                     # Test utilities
│   │   └── mockregistry.go           # Mock container registry server
│   └── version/
│       └── version.go
```

## Testing Strategy

**Integration tests are the preferred way to test and develop new features.** They provide true end-to-end coverage with a real (mock) registry, ensuring the entire pipeline works correctly.

### Integration Tests (`internal/integration/`)

Integration tests use a mock container registry server (`internal/testutil/mockregistry.go`) that:
- Serves deterministic images with reproducible digests
- Tracks all requests for assertions (verify the registry was actually hit)
- Uses `go-containerregistry/pkg/registry` for a real OCI registry implementation

**How it works:**
1. `TestMain` builds the CLI binary and starts the mock registry
2. Images are added to the mock registry with deterministic content (using `empty.Image` + fixed labels)
3. A `registries.conf` file redirects `docker.io`, `ghcr.io`, etc. to the mock server
4. Tests run the CLI binary with `CONTAINERS_REGISTRIES_CONF` env var pointing to mock config
5. Snapshots (`go-snaps`) verify the JSON output

**Adding a new test case:**
1. Create a new directory under `internal/integration/testdata/` with a `Dockerfile`
2. Add any required images to the mock registry in `TestMain`
3. Add a test case to `TestPin` with expected image paths
4. Run `UPDATE_SNAPS=true go test ./internal/integration/...` to generate snapshots

### Unit Tests

- Standard Go tests for isolated parsing logic (`internal/dockerfile/parser_test.go`)
- Use when testing pure functions that don't require registry interaction

### Test Fixtures

Test fixtures are organized in separate directories under `testdata/` to support future context-aware features (dockerignore, config files, etc.)

## Key Flags

- `--output, -o`: Write policy to file
- `--stdout`: Write policy to stdout

## Registry Interaction

The tool uses `github.com/containers/image/v5` for registry interaction, which respects:
- `CONTAINERS_REGISTRIES_CONF` environment variable (path to custom `registries.conf`)
- `registries.conf` configuration (podman-style registry redirection)
- Docker credential helpers
- System certificates

The `CONTAINERS_REGISTRIES_CONF` support enables testing with a mock registry by redirecting all registry requests.

## BuildKit Source Policy Format

Output follows the BuildKit source policy protobuf schema with JSON encoding:
```json
{
  "version": 1,
  "rules": [
    {
      "action": "CONVERT",
      "selector": {
        "identifier": "docker-image://alpine:3.18",
        "matchType": "EXACT"
      },
      "updates": {
        "identifier": "docker-image://docker.io/library/alpine:3.18@sha256:..."
      }
    }
  ]
}
```
