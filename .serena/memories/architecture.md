# Architecture

## Package Structure

```
internal/
├── dockerfile/   # Dockerfile parsing (wraps buildkit parser)
│   └── parser.go # ParseFile(), Parse(), ImageRef struct
├── registry/     # Registry interaction (wraps containers/image)
│   └── client.go # Client.GetDigest()
├── policy/       # BuildKit source policy types
│   └── types.go  # Policy, Rule structs, JSON marshaling
├── pin/          # Pin operation orchestration
│   └── pin.go    # GeneratePolicy(), WritePolicy()
├── integration/  # Integration tests with go-snaps
│   └── testdata/ # Each fixture in own directory for future context support
└── version/      # Version information (set at build time)
```

## Data Flow (pin command)
1. Parse Dockerfiles → `[]dockerfile.ImageRef`
2. For each ref without digest: `registry.Client.GetDigest()` → digest string
3. Build `policy.Policy` with pin rules
4. Marshal to JSON and write to output

## Key Types
- `dockerfile.ImageRef`: Original string, normalized `reference.Named`, line number, stage name
- `policy.Policy`: BuildKit-compatible source policy with version and rules
- `policy.Rule`: CONVERT action with selector (EXACT match) and updates

## Testing
- Unit tests: `internal/dockerfile/parser_test.go`
- Integration tests: `internal/integration/integration_test.go` (go-snaps snapshots)
- `--dry-run` flag enables testing without registry calls
