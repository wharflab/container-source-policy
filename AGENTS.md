# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI for generating BuildKit source policy files (`docker buildx build --source-policy-file …`) by parsing Dockerfiles and pinning image references to digests.

- `main.go`: application entrypoint
- `cmd/container-source-policy/cmd/`: Cobra commands (`root.go`, `pin.go`, `version.go`)
- `internal/`: implementation packages (Dockerfile parsing, registry client, policy types, pin logic)
- `internal/integration/`: end-to-end tests with snapshots and fixtures
  - `internal/integration/testdata/<case>/Dockerfile`: test Dockerfiles
  - `internal/integration/__snapshots__/`: `go-snaps` snapshot outputs
- `bin/` and `dist/`: local tools / release artifacts (ignored by Git)

## Build, Test, and Development Commands

- `make build`: builds the `container-source-policy` binary into the repo root
- `make test`: runs `go test -race -count=1 -timeout=30s ./...`
- `make lint`: runs `golangci-lint` (with `--fix`) and enforces formatting
- `make clean`: removes the built binary and deletes `bin/` + `dist/`

Local usage examples:
- `go run . pin --help`
- `go run . pin --stdout Dockerfile`

## Coding Style & Naming Conventions

- Format: `gofmt` + `goimports` (configured via `.golangci.yaml`, with `github.com/tinovyatkin/container-source-policy` as the local import prefix).
- Prefer small, focused packages under `internal/`; keep CLI wiring in `cmd/`.
- Tests use standard Go conventions: filenames end in `*_test.go`.

## Testing Guidelines

- Unit tests live alongside packages in `internal/**`.
- Integration tests (`internal/integration`) build the binary once and run it against a local mock registry (no real registry calls).
- Update snapshots when intentional output changes:
  - `UPDATE_SNAPS=true go test ./internal/integration/...`

## Commit & Pull Request Guidelines

- Follow the repo’s existing convention: short, imperative Conventional Commit-style messages like `feat: …`, `fix: …`, `chore: …`.
- Run `make lint` and `make test` before opening a PR (Lefthook runs these on `pre-commit` and `make build` on `pre-push`).
- PRs should explain *what* changed and *why*, note any snapshot updates, and avoid committing build outputs (the `container-source-policy` binary is Git-ignored).
