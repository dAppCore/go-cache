# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`go-cache` is a storage-agnostic, JSON-based caching library for Go. Module path: `forge.lthn.ai/core/go-cache`. The entire package is two files: `cache.go` and `cache_test.go`.

## Commands

```bash
# Run all tests
go test ./...

# Run a single test
go test -run TestCache ./...

# QA pipeline (fmt + vet + lint + test) — requires `core` CLI
core go qa
core go qa full    # adds race detector, vuln scan, security audit

# Individual checks
core go fmt
core go vet
core go lint

# Coverage
core go cov
core go cov --open
```

## Key Architecture Details

- All I/O is delegated to the `io.Medium` interface from `forge.lthn.ai/core/go-io` — the cache never reads/writes files directly. This makes it backend-swappable (local FS, SQLite, S3, in-memory mock).
- `Cache.Path()` enforces path-traversal protection on every public method — keys like `../../etc/passwd` are rejected before any I/O occurs.
- Expired entries are not eagerly deleted; they remain on disk until overwritten or explicitly removed.
- The struct has no mutex. Concurrent reads are safe, but concurrent writes to the same key need external synchronization.

## Testing Conventions

- Use `io.NewMockMedium()` for all tests — no real filesystem access.
- Use `testing.T` directly, not testify.
- Use short TTLs (milliseconds) for expiry tests.

## Commit Conventions

Conventional commits: `feat(cache):`, `fix(cache):`, `refactor:`, etc. The release config (`.core/release.yaml`) includes `feat`, `fix`, `perf`, `refactor` in changelogs.

## Environment

- Go 1.26+
- Private modules: `GOPRIVATE=forge.lthn.ai/*`
- Licence: EUPL-1.2
