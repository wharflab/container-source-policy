# httpchecksum

Package `httpchecksum` provides an HTTP client for computing checksums of remote resources with optimizations for common hosting providers.

## Features

- **Optimized checksum retrieval** - avoids downloading full content when possible:
  - **AWS S3**: Uses `X-Amz-Checksum-Sha256` header
  - **GitHub Releases**: Uses GitHub API to fetch asset digests
  - **raw.githubusercontent.com**: Uses ETag header (SHA256)
  - **Other servers**: Downloads and computes SHA256

- **Cache validation** - detects volatile content that shouldn't be pinned:
  - Checks `Cache-Control`, `Expires`, and `Pragma` headers
  - Returns `VolatileContentError` for content with `no-store`, `no-cache`, or `max-age=0`

- **Progress reporting** - optional callback for tracking download progress

- **Header tracking** - captures HTTP headers that affect response content (via `Vary` header)

## Installation

```bash
go get github.com/tinovyatkin/container-source-policy/httpchecksum
```

## Usage

### Basic checksum computation

```go
import "github.com/tinovyatkin/container-source-policy/httpchecksum"

client := httpchecksum.NewClient()
checksum, err := client.GetChecksum(ctx, "https://example.com/file.tar.gz")
if err != nil {
    log.Fatal(err)
}
fmt.Println(checksum) // Output: sha256:...
```

### With HTTP headers

```go
result, err := client.GetChecksumWithHeaders(ctx, "https://example.com/file.tar.gz")
if err != nil {
    log.Fatal(err)
}
fmt.Println("Checksum:", result.Checksum)
fmt.Println("Headers:", result.Headers) // Headers from Vary response header
```

### With progress reporting

```go
clientWithProgress := client.WithProgressFactory(func(contentLength int64) io.Writer {
    // Return a writer that receives download progress
    // You can use a progress bar library here
    bar := progressbar.NewBar(contentLength)
    return bar
})

checksum, err := clientWithProgress.GetChecksum(ctx, "https://example.com/large-file.tar.gz")
```

### GitHub token authentication

For GitHub releases, set the `GITHUB_TOKEN` environment variable to increase rate limits:

```bash
export GITHUB_TOKEN=ghp_...
```

This increases the rate limit from 60 requests/hour (unauthenticated) to 5,000 requests/hour (authenticated).

## Error Handling

The package provides specific error types for common scenarios:

### Authentication errors

```go
checksum, err := client.GetChecksum(ctx, url)
if httpchecksum.IsAuthError(err) {
    log.Println("Authentication required for", url)
}
```

### Volatile content errors

```go
checksum, err := client.GetChecksum(ctx, url)
if httpchecksum.IsVolatileContentError(err) {
    log.Println("Content is volatile and shouldn't be pinned:", err)
}
```

## Testing

The package includes comprehensive tests with mock servers:

```bash
go test ./httpchecksum/...
```

## Design Philosophy

This package is designed to minimize code ownership by heavily reusing well-maintained libraries:

- Uses standard library `net/http` for HTTP operations
- Uses `github.com/pquerna/cachecontrol` for RFC 7234 compliant cache header parsing

All optimizations are transparent fallbacks - if a server-provided checksum is unavailable or invalid, the client automatically falls back to
downloading and computing the checksum.
