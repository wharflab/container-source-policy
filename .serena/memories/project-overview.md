# container-source-policy Project Overview

## Purpose
CLI tool for generating BuildKit source policy files (`--source-policy-file` input for `docker buildx build`). Parses Dockerfiles to extract image references and pins them to their current digests.

## Design Philosophy
**Minimize code ownership** - heavily reuses existing libraries:
- `github.com/moby/buildkit/frontend/dockerfile/parser` - Official Dockerfile parsing
- `github.com/containers/image/v5` - Image reference parsing, registry interaction
- `github.com/spf13/cobra` - CLI framework
- `github.com/gkampitakis/go-snaps` - Snapshot testing

## Commands
- `pin`: Generate source policy with pinned digests
  - `--output, -o`: Write to file
  - `--stdout`: Write to stdout
  - `--dry-run`: Parse without fetching digests

## Future Commands (Planned)
- `update`: Update existing policy file with new digests
- `check`: Verify policy file against current digests
