---
title: Architecture
description: Internals of go-cache -- types, data flow, storage format, and security model.
---

# Architecture

This document explains how `go-cache` works internally, covering its type
system, on-disc format, data flow, and security considerations.


## Core Types

### Cache

```go
type Cache struct {
    medium  io.Medium
    baseDir string
    ttl     time.Duration
}
```

`Cache` is the primary handle. It holds:

- **medium** -- the storage backend (any `io.Medium` implementation).
- **baseDir** -- the root directory under which all cache files live.
- **ttl** -- how long entries remain valid after being written.

All three fields are set once during construction via `cache.New()` and are
immutable for the lifetime of the instance.


### Entry

```go
type Entry struct {
    Data      json.RawMessage `json:"data"`
    CachedAt  time.Time       `json:"cached_at"`
    ExpiresAt time.Time       `json:"expires_at"`
}
```

`Entry` is the envelope written to storage. It wraps the caller's data as raw
JSON and adds two timestamps for expiry tracking. Using `json.RawMessage` means
the data payload is stored verbatim -- no intermediate deserialisation happens
during writes.


## Constructor Defaults

`cache.New(medium, baseDir, ttl)` applies sensible defaults when arguments are
zero-valued:

| Parameter | Zero value   | Default applied                             |
|-----------|--------------|---------------------------------------------|
| `medium`  | `nil`        | `io.Local` (unsandboxed local filesystem)   |
| `baseDir` | `""`         | `.core/cache/` relative to the working dir  |
| `ttl`     | `0`          | `cache.DefaultTTL` (1 hour)                 |

The constructor also calls `medium.EnsureDir(baseDir)` to guarantee the cache
directory exists before any reads or writes.


## Data Flow

### Writing (`Set`)

```
caller data
    |
    v
json.Marshal(data)           -- serialise caller's value
    |
    v
wrap in Entry{               -- add timestamps
    Data:      <marshalled>,
    CachedAt:  time.Now(),
    ExpiresAt: time.Now().Add(ttl),
}
    |
    v
json.MarshalIndent(entry)    -- human-readable JSON
    |
    v
medium.Write(path, string)   -- persist via the storage backend
```

The resulting file on disc (or equivalent record in another medium) looks like:

```json
{
  "data": { "foo": "bar" },
  "cached_at": "2026-03-10T14:30:00Z",
  "expires_at": "2026-03-10T15:30:00Z"
}
```

Parent directories for nested keys (e.g. `github/host-uk/repos`) are created
automatically via `medium.EnsureDir()`.


### Reading (`Get`)

```
medium.Read(path)
    |
    v
json.Unmarshal -> Entry       -- parse the envelope
    |
    v
time.Now().After(ExpiresAt)?  -- check TTL
    |                |
   yes              no
    |                |
    v                v
return false    json.Unmarshal(entry.Data, dest)
(cache miss)         |
                     v
                return true
                (cache hit)
```

Key behaviours:

- If the file does not exist (`os.ErrNotExist`), `Get` returns `(false, nil)` --
  a miss, not an error.
- If the file contains invalid JSON, it is treated as a miss (not an error).
  This prevents corrupted files from blocking the caller.
- If the entry exists but has expired, it is treated as a miss. The stale file
  is **not** deleted eagerly -- it remains on disc until explicitly removed or
  overwritten.


### Deletion

- **`Delete(key)`** removes a single entry. If the file does not exist, the
  operation succeeds silently.
- **`DeleteMany(keys...)`** removes several entries in one call and ignores
  missing files, using the same per-key path validation as `Delete()`.
- **`Clear()`** calls `medium.DeleteAll(baseDir)`, removing the entire cache
  directory and all its contents.


### Age Inspection

`Age(key)` returns the `time.Duration` since the entry was written (`CachedAt`).
If the entry does not exist or cannot be parsed, it returns `-1`. This is useful
for diagnostics without triggering the expiry check that `Get` performs.


## Key-to-Path Mapping

Cache keys are mapped to file paths by appending `.json` and joining with the
base directory:

```
key:  "github/host-uk/repos"
path: <baseDir>/github/host-uk/repos.json
```

Keys may contain forward slashes to create a directory hierarchy. This is how
the GitHub key helpers work:

```go
func GitHubReposKey(org string) string {
    return core.JoinPath("github", org, "repos")
}

func GitHubRepoKey(org, repo string) string {
    return core.JoinPath("github", org, repo, "meta")
}
```


## Security: Path Traversal Prevention

The `Path()` method guards against directory traversal attacks. After computing
the full path, it resolves both the base directory and the result to absolute
paths, then checks that the result is still a prefix of the base:

```go
if !core.HasPrefix(absPath, absBase+pathSeparator()) && absPath != absBase {
    return "", coreerr.E("cache.Path", "invalid cache key: path traversal attempt", nil)
}
```

This means a key like `../../etc/passwd` will be rejected before any I/O
occurs. Every public method (`Get`, `Set`, `Delete`, `Age`) calls `Path()`
internally, so traversal protection is always active.


## Concurrency

The `Cache` struct does not include a mutex. Concurrent reads are safe (each
call does independent file I/O), but concurrent writes to the **same key** may
produce a race at the filesystem level. If your application writes to the same
key from multiple goroutines, protect the call site with your own
synchronisation.

In practice, caches in this ecosystem are typically written by a single
goroutine (e.g. a CLI command fetching GitHub data) and read by others, which
avoids contention.


## Relationship to go-io

`go-cache` delegates all storage operations to the `io.Medium` interface from
`go-io`. It uses only five methods:

| Method       | Used by             |
|--------------|---------------------|
| `EnsureDir`  | `New`, `Set`        |
| `Read`       | `Get`, `Age`        |
| `Write`      | `Set`               |
| `Delete`     | `Delete`            |
| `DeleteAll`  | `Clear`             |

This minimal surface makes it straightforward to swap storage backends. For
tests, `io.NewMockMedium()` provides a fully in-memory implementation with no
disc access.
