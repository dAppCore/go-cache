---
title: Development
description: Building, testing, and contributing to go-cache.
---

# Development

This guide covers how to build, test, and contribute to `go-cache`.


## Prerequisites

- **Go 1.26** or later
- Access to `forge.lthn.ai` modules (`GOPRIVATE=forge.lthn.ai/*`)
- The `core` CLI (optional, for `core go test` and `core go qa`)


## Getting the Source

```bash
git clone ssh://git@forge.lthn.ai:2223/core/go-cache.git
cd go-cache
```

If you are working within the Go workspace at `~/Code/go.work`, the module is
already available locally and dependency resolution will use workspace overrides.


## Running Tests

With the `core` CLI:

```bash
core go test
```

With plain Go:

```bash
go test ./...
```

To run a single test:

```bash
core go test --run TestCache
# or
go test -run TestCache ./...
```

The test suite uses `io.NewMockMedium()` for all storage operations, so no
files are written to disc and tests run quickly in any environment.


## Test Coverage

```bash
core go cov           # Generate coverage report
core go cov --open    # Generate and open in browser
```


## Code Quality

The full QA pipeline runs formatting, vetting, linting, and tests in one
command:

```bash
core go qa            # fmt + vet + lint + test
core go qa full       # adds race detector, vulnerability scan, security audit
```

Individual steps:

```bash
core go fmt           # Format with gofmt
core go vet           # Static analysis
core go lint          # Linter checks
```


## Project Structure

```
go-cache/
  .core/
    build.yaml        # Build configuration (targets, flags)
    release.yaml      # Release configuration (changelog rules)
  cache.go            # Package source
  cache_test.go       # Tests
  go.mod              # Module definition
  go.sum              # Dependency checksums
  docs/               # This documentation
```

The package is intentionally small -- a single source file and a single test
file. There are no sub-packages.


## Writing Tests

Tests follow the standard Go testing conventions. The codebase uses
`testing.T` directly (not testify assertions) for simplicity. When adding tests:

1. Use `io.NewMockMedium()` rather than the real filesystem.
2. Keep TTLs short (milliseconds) when testing expiry behaviour.
3. Name test functions descriptively: `TestCacheExpiry`, `TestCacheDefaults`, etc.

Example of testing cache expiry:

```go
func TestCacheExpiry(t *testing.T) {
    m := io.NewMockMedium()
    c, err := cache.New(m, "/tmp/test", 10*time.Millisecond)
    if err != nil {
        t.Fatalf("failed to create cache: %v", err)
    }

    c.Set("key", "value")
    time.Sleep(50 * time.Millisecond)

    var result string
    found, _ := c.Get("key", &result)
    if found {
        t.Error("expected expired entry to be a cache miss")
    }
}
```


## Commit Conventions

This project uses conventional commits:

```
feat(cache): add batch eviction support
fix(cache): handle corrupted JSON gracefully
refactor: simplify Path() traversal check
```

The release configuration (`.core/release.yaml`) includes `feat`, `fix`,
`perf`, and `refactor` in changelogs, and excludes `chore`, `docs`, `style`,
`test`, and `ci`.


## Build Configuration

The `.core/build.yaml` defines cross-compilation targets:

| OS      | Architecture |
|---------|-------------|
| Linux   | amd64       |
| Linux   | arm64       |
| Darwin  | arm64       |
| Windows | amd64       |

Since `go-cache` is a library (no `main` package), the build configuration is
primarily used by the CI pipeline for compilation checks rather than producing
binaries.


## Adding a New Storage Backend

To use the cache with a different storage medium, implement the `io.Medium`
interface from `go-io` and pass it to `cache.New()`. The cache only requires
five methods: `EnsureDir`, `Read`, `Write`, `Delete`, and `DeleteAll`. See
the [architecture](architecture.md) document for the full method mapping.

```go
import (
    "forge.lthn.ai/core/go-cache"
    "forge.lthn.ai/core/go-io/store"
    "time"
)

// Use SQLite as the cache backend
medium, err := store.NewMedium("/path/to/cache.db")
if err != nil {
    panic(err)
}

c, err := cache.New(medium, "cache", 30*time.Minute)
```


## Licence

EUPL-1.2. See the repository root for the full licence text.
