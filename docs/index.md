---
title: go-cache
description: File-based caching with TTL expiry, storage-agnostic via the go-io Medium interface.
---

# go-cache

`go-cache` is a lightweight, storage-agnostic caching library for Go. It stores
JSON-serialised entries with automatic TTL expiry and path-traversal protection.

**Module path:** `forge.lthn.ai/core/go-cache`

**Licence:** EUPL-1.2


## Quick Start

```go
import (
    "fmt"
    "time"

    "forge.lthn.ai/core/go-cache"
)

func main() {
    // Create a cache with default settings:
    //   - storage: local filesystem (io.Local)
    //   - directory: .core/cache/ in the working directory
    //   - TTL: 1 hour
    c, err := cache.New(nil, "", 0)
    if err != nil {
        panic(err)
    }

    // Store a value
    err = c.Set("user/profile", map[string]string{
        "name": "Alice",
        "role": "admin",
    })
    if err != nil {
        panic(err)
    }

    // Retrieve it (returns false if missing or expired)
    var profile map[string]string
    found, err := c.Get("user/profile", &profile)
    if err != nil {
        panic(err)
    }
    if found {
        fmt.Println(profile["name"]) // Alice
    }
}
```


## Package Layout

| File            | Purpose                                                     |
|-----------------|-------------------------------------------------------------|
| `cache.go`      | Core types (`Cache`, `Entry`), CRUD operations, key helpers |
| `cache_test.go` | Tests covering set/get, expiry, delete, clear, defaults     |
| `go.mod`        | Module definition (Go 1.26)                                 |


## Dependencies

| Module                        | Version | Role                                       |
|-------------------------------|---------|---------------------------------------------|
| `forge.lthn.ai/core/go-io`   | v0.0.3  | Storage abstraction (`Medium` interface)    |
| `forge.lthn.ai/core/go-log`  | v0.0.1  | Structured logging (indirect, via `go-io`)  |

There are no other runtime dependencies. The test suite uses the standard
library only (plus the `MockMedium` from `go-io`).


## Key Concepts

### Storage Backends

The cache does not read or write files directly. All I/O goes through the
`io.Medium` interface defined in `go-io`. This means the same cache logic works
against:

- **Local filesystem** (`io.Local`) -- the default
- **SQLite KV store** (`store.Medium` from `go-io/store`)
- **S3-compatible storage** (`go-io/s3`)
- **In-memory mock** (`io.NewMockMedium()`) -- ideal for tests

Pass any `Medium` implementation as the first argument to `cache.New()`.

### TTL and Expiry

Every entry records both `cached_at` and `expires_at` timestamps. On `Get()`,
if the current time is past `expires_at`, the entry is treated as a cache miss
-- no stale data is ever returned. The default TTL is one hour
(`cache.DefaultTTL`).

### GitHub Cache Keys

The package includes two helper functions that produce consistent cache keys
for GitHub API data:

```go
cache.GitHubReposKey("host-uk")          // "github/host-uk/repos"
cache.GitHubRepoKey("host-uk", "core")   // "github/host-uk/core/meta"
```

These are convenience helpers used by other packages in the ecosystem (such as
`go-devops`) to avoid key duplication when caching GitHub responses.
