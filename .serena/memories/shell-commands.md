# Common Shell Commands

## Build
```bash
go build ./...
```

## Test
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test ./... -v

# Update snapshots for integration tests
UPDATE_SNAPS=true go test ./internal/integration/...
```

## Run
```bash
# Show help
go run . pin --help

# Dry run (no registry calls)
go run . pin --dry-run --stdout Dockerfile

# Generate policy
go run . pin --stdout Dockerfile
go run . pin --output policy.json Dockerfile
```

## Dependencies
```bash
go mod tidy
```
