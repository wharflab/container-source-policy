# container-source-policy

Generate a Docker BuildKit **source policy** file (`docker buildx build --source-policy-file …`) by parsing Dockerfiles and pinning `FROM` images to immutable digests.

This helps make `docker buildx build` inputs reproducible without rewriting your Dockerfile.

## Quick start

```bash
container-source-policy pin --stdout Dockerfile > source-policy.json
docker buildx build --source-policy-file source-policy.json -t my-image:dev .
```

## Install

### Go (build from source)

```bash
go install github.com/tinovyatkin/container-source-policy@latest
```

### npm (prebuilt binary)

```bash
npm i -g container-source-policy
container-source-policy --help
```

### PyPI (prebuilt binary)

```bash
pipx install container-source-policy
container-source-policy --help
```

### RubyGems (prebuilt binary)

```bash
gem install container-source-policy
container-source-policy --help
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

- Looks at `FROM …` instructions across all provided Dockerfiles.
- Skips:
  - `FROM scratch`
  - `FROM <stage>` references to a previous named build stage
  - `FROM ${VAR}` / `FROM $VAR` (unexpanded ARG/ENV variables)
  - images already written as `name@sha256:…`
- Resolves the image manifest digest from the registry and emits BuildKit `CONVERT` rules of the form:
  - `docker-image://<as-written-in-Dockerfile>` → `docker-image://<normalized>@sha256:…`

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
- `internal/dockerfile`: Dockerfile parsing (`FROM` extraction)
- `internal/registry`: registry client (digest resolution)
- `internal/policy`: BuildKit source policy types and JSON output
- `internal/pin`: orchestration logic for `pin`
- `internal/integration`: end-to-end tests with a mock registry and snapshots
- `packaging/`: wrappers for publishing prebuilt binaries to npm / PyPI / RubyGems

## Packaging

See `packaging/README.md` for how the npm/PyPI/Ruby packages are assembled from GoReleaser artifacts.
