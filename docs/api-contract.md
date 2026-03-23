---
title: API Contract
description: Exported API contract for dappco.re/go/core/cache.
---

# API Contract

This table lists every exported constant, type, function, and method in
`dappco.re/go/core/cache`.

`Test coverage` is `yes` when the export is directly exercised by
`cache_test.go`. `Usage-example comment` is `yes` only when the symbol has its
own usage example in a doc comment or Go example test.

| Name | Signature | Package Path | Description | Test Coverage | Usage-Example Comment |
|------|-----------|--------------|-------------|---------------|-----------------------|
| `DefaultTTL` | `const DefaultTTL = 1 * time.Hour` | `dappco.re/go/core/cache` | Default cache expiry time. | no | no |
| `Cache` | `type Cache struct { /* unexported fields */ }` | `dappco.re/go/core/cache` | File-based cache handle. | yes | no |
| `Entry` | `type Entry struct { Data json.RawMessage; CachedAt time.Time; ExpiresAt time.Time }` | `dappco.re/go/core/cache` | Cached item envelope with payload and timestamps. | no | no |
| `New` | `func New(medium coreio.Medium, baseDir string, ttl time.Duration) (*Cache, error)` | `dappco.re/go/core/cache` | Creates a cache instance, applying default medium, base directory, and TTL when zero-valued inputs are provided. | yes | no |
| `(*Cache).Path` | `func (c *Cache) Path(key string) (string, error)` | `dappco.re/go/core/cache` | Returns the full path for a cache key and rejects path traversal. | yes | no |
| `(*Cache).Get` | `func (c *Cache) Get(key string, dest any) (bool, error)` | `dappco.re/go/core/cache` | Retrieves a cached item if it exists and has not expired. | yes | no |
| `(*Cache).Set` | `func (c *Cache) Set(key string, data any) error` | `dappco.re/go/core/cache` | Stores an item in the cache. | yes | no |
| `(*Cache).Delete` | `func (c *Cache) Delete(key string) error` | `dappco.re/go/core/cache` | Removes an item from the cache. | yes | no |
| `(*Cache).Clear` | `func (c *Cache) Clear() error` | `dappco.re/go/core/cache` | Removes all cached items. | yes | no |
| `(*Cache).Age` | `func (c *Cache) Age(key string) time.Duration` | `dappco.re/go/core/cache` | Returns how old a cached item is, or `-1` if it is not cached. | yes | no |
| `GitHubReposKey` | `func GitHubReposKey(org string) string` | `dappco.re/go/core/cache` | Returns the cache key for an organization's repo list. | yes | no |
| `GitHubRepoKey` | `func GitHubRepoKey(org, repo string) string` | `dappco.re/go/core/cache` | Returns the cache key for a specific repo's metadata. | yes | no |
