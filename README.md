# container-source-policy

Generate a Docker BuildKit **source policy** file by parsing Dockerfiles and pinning `FROM` images to immutable digests.

This helps make `docker buildx build` inputs reproducible without rewriting your Dockerfile.

See the [BuildKit documentation on build reproducibility](https://github.com/moby/buildkit/blob/master/docs/build-repro.md) for more details on source policies.

## Quick start

```bash
container-source-policy pin --stdout Dockerfile > source-policy.json
EXPERIMENTAL_BUILDKIT_SOURCE_POLICY=source-policy.json docker buildx build -t my-image:dev .
```

> **Note:** [`EXPERIMENTAL_BUILDKIT_SOURCE_POLICY`](https://docs.docker.com/build/building/variables/#experimental_buildkit_source_policy) is the environment variable used by Docker Buildx to specify a source policy file.

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

Then pass the policy to BuildKit / Buildx via the environment variable:

```bash
EXPERIMENTAL_BUILDKIT_SOURCE_POLICY=source-policy.json docker buildx build .
```

Or use `buildctl` directly with the `--source-policy-file` flag:

```bash
buildctl build --frontend dockerfile.v0 --local dockerfile=. --local context=. --source-policy-file source-policy.json
```

Shell completion scripts are available via Cobra:

```bash
container-source-policy completion zsh
```

## What gets pinned

### Container images (`FROM` and `COPY --from`)

- Looks at `FROM …` and `COPY --from=<image>` instructions across all provided Dockerfiles.
- Skips:
  - `FROM scratch`
  - `FROM <stage>` / `COPY --from=<stage>` references to a previous named build stage
  - `COPY --from=0` numeric stage indices
  - `FROM ${VAR}` / `COPY --from=${VAR}` (unexpanded ARG/ENV variables)
  - images already written as `name@sha256:…`
- Resolves the image manifest digest from the registry and emits BuildKit `CONVERT` rules of the form:
  - `docker-image://<as-written-in-Dockerfile>` → `docker-image://<normalized>@sha256:…`

### HTTP sources (`ADD`)

- Looks at `ADD <url> …` instructions with HTTP/HTTPS URLs.
- Skips:
  - `ADD --checksum=… <url>` (already pinned)
  - URLs containing unexpanded variables (`${VAR}`, `$VAR`)
  - Git URLs (handled separately, see below)
- Fetches the checksum and emits `CONVERT` rules with `http.checksum` attribute.
- **Respects `Vary` header**: captures request headers that affect response content (e.g., `User-Agent`, `Accept-Encoding`) and includes them in the policy as `http.header.*` attributes to ensure reproducible builds.

**Optimized checksum fetching** — avoids downloading large files when possible:
- `raw.githubusercontent.com`: extracts SHA256 from ETag header
- GitHub releases: uses the API `digest` field (set `GITHUB_TOKEN` for higher rate limits)
- S3: uses `x-amz-checksum-sha256` response header (by sending `x-amz-checksum-mode: ENABLED`)
- Fallback: downloads and computes SHA256

### Git sources (`ADD`)

- Looks at `ADD <git-url> …` instructions with Git repository URLs.
- Supports various Git URL formats:
  - `https://github.com/owner/repo.git#ref`
  - `git://host/path#ref`
  - `git@github.com:owner/repo#ref`
  - `ssh://git@host/path#ref`
- Skips URLs containing unexpanded variables (`${VAR}`, `$VAR`)
- Uses `git ls-remote` to resolve the ref (branch, tag, or commit) to a commit SHA
- Emits `CONVERT` rules with `git.checksum` attribute (full 40-character commit SHA)

Example: `ADD https://github.com/cli/cli.git#v2.40.0 /dest` pins to commit `54d56cab...`

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
- `internal/git`: Git client (commit SHA resolution via git ls-remote)
- `internal/policy`: BuildKit source policy types and JSON output
- `internal/pin`: orchestration logic for `pin`
- `internal/integration`: end-to-end tests with mock registry/HTTP server and snapshots
- `packaging/`: wrappers for publishing prebuilt binaries to npm / PyPI / RubyGems

## Packaging

See `packaging/README.md` for how the npm/PyPI/Ruby packages are assembled from GoReleaser artifacts.
