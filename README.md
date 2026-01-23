# container-source-policy

Generate a Docker BuildKit **source policy** file (`docker buildx build --source-policy-file …`) by parsing Dockerfiles and pinning `FROM` images to immutable digests.

This helps make `docker buildx build` inputs reproducible without rewriting your Dockerfile.

## Quick start

```bash
container-source-policy pin --stdout Dockerfile > source-policy.json
docker buildx build --source-policy-file source-policy.json -t my-image:dev .
```

## Install

Run directly without installing (recommended):

```bash
# npm/bun
npx container-source-policy --help
bunx container-source-policy --help

# Python
uvx container-source-policy --help

# Ruby (requires RubyGems 3.3+)
gem exec container-source-policy --help
```

Or install globally:

```bash
# Go (build from source)
go install github.com/tinovyatkin/container-source-policy@latest

# npm
npm i -g container-source-policy

# Python
pipx install container-source-policy

# Ruby
gem install container-source-policy
```

## Usage

Generate a policy for one or more Dockerfiles:

```bash
container-source-policy pin --stdout Dockerfile Dockerfile.ci > source-policy.json
```

Read the Dockerfile from stdin:

```bash
cat Dockerfile | container-source-policy pin --stdout -
```

Write directly to a file:

```bash
container-source-policy pin --output source-policy.json Dockerfile
```

Then pass the policy to BuildKit / Buildx:

```bash
docker buildx build --source-policy-file source-policy.json .
```

Shell completion scripts are available via Cobra:

```bash
container-source-policy completion zsh
```

## What gets pinned

### Container images (`FROM`)

- Looks at `FROM …` instructions across all provided Dockerfiles.
- Skips:
  - `FROM scratch`
  - `FROM <stage>` references to a previous named build stage
  - `FROM ${VAR}` / `FROM $VAR` (unexpanded ARG/ENV variables)
  - images already written as `name@sha256:…`
- Resolves the image manifest digest from the registry and emits BuildKit `CONVERT` rules of the form:
  - `docker-image://<as-written-in-Dockerfile>` → `docker-image://<normalized>@sha256:…`

### HTTP sources (`ADD`)

- Looks at `ADD <url> …` instructions with HTTP/HTTPS URLs.
- Skips:
  - `ADD --checksum=… <url>` (already pinned)
  - URLs containing unexpanded variables (`${VAR}`, `$VAR`)
- Fetches the checksum and emits `CONVERT` rules with `http.checksum` attribute.

**Optimized checksum fetching** — avoids downloading large files when possible:
- `raw.githubusercontent.com`: extracts SHA256 from ETag header
- GitHub releases: uses the API `digest` field (set `GITHUB_TOKEN` for higher rate limits)
- S3: uses `x-amz-checksum-sha256` response header (by sending `x-amz-checksum-mode: ENABLED`)
- Fallback: downloads and computes SHA256

## Development

```bash
make build
make test
make lint
```

Update integration-test snapshots:

```bash
UPDATE_SNAPS=true go test ./internal/integration/...
```

## Repository layout

- `cmd/container-source-policy/cmd/`: Cobra CLI commands
- `internal/dockerfile`: Dockerfile parsing (`FROM` and `ADD` extraction)
- `internal/registry`: registry client (image digest resolution)
- `internal/http`: HTTP client (URL checksum fetching with optimizations)
- `internal/policy`: BuildKit source policy types and JSON output
- `internal/pin`: orchestration logic for `pin`
- `internal/integration`: end-to-end tests with mock registry/HTTP server and snapshots
- `packaging/`: wrappers for publishing prebuilt binaries to npm / PyPI / RubyGems

## Packaging

See `packaging/README.md` for how the npm/PyPI/Ruby packages are assembled from GoReleaser artifacts.
