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
go run . pin --dry-run --stdout Dockerfile
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
│   └── version/
│       └── version.go
```

## Testing Strategy

- **Unit tests**: Standard Go tests for parsing logic (`internal/dockerfile/parser_test.go`)
- **Integration tests**: Snapshot-based tests using `go-snaps` that execute the binary with fixture Dockerfiles and compare stdout output (`internal/integration/`)
- Test fixtures are organized in separate directories under `testdata/` to support future context-aware features (dockerignore, config files, etc.)

## Key Flags

- `--output, -o`: Write policy to file
- `--stdout`: Write policy to stdout
- `--dry-run`: Parse without fetching digests (uses placeholder, for testing)

## Registry Interaction

The tool uses `github.com/containers/image/v5` for registry interaction, which respects:
- `registries.conf` configuration
- Docker credential helpers
- System certificates

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
