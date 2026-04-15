---
title: API Contract
description: Exported API contract for dappco.re/go/cache.
---

# API Contract

This table lists every exported constant, type, function, and method in
`dappco.re/go/cache`.

`Test coverage` is `yes` when the export is directly exercised by
`cache_test.go`. `Usage-example comment` is `yes` only when the symbol has its
own usage example in a doc comment or Go example test.

| Name | Signature | Package Path | Description | Test Coverage | Usage-Example Comment |
|------|-----------|--------------|-------------|---------------|-----------------------|
| `DefaultTTL` | `const DefaultTTL = 1 * time.Hour` | `dappco.re/go/cache` | Default cache expiry time. | no | no |
| `Cache` | `type Cache struct { /* unexported fields */ }` | `dappco.re/go/cache` | File-based cache handle. | yes | no |
| `Entry` | `type Entry struct { Data json.RawMessage; CachedAt time.Time; ExpiresAt time.Time }` | `dappco.re/go/cache` | Cached item envelope with payload and timestamps. | no | no |
| `BinaryMeta` | `type BinaryMeta struct { ContentType string; Size int64; CachedAt time.Time; ExpiresAt time.Time }` | `dappco.re/go/cache` | Metadata envelope for binary cache payloads. | yes | no |
| `InvalidateFunc` | `type InvalidateFunc func(trigger string) []string` | `dappco.re/go/cache` | Callback signature used for cache invalidation triggers. | no | no |
| `CacheStorage` | `type CacheStorage struct { /* unexported fields */ }` | `dappco.re/go/cache` | Named HTTP cache storage container. | yes | no |
| `HTTPCache` | `type HTTPCache struct { /* unexported fields */ }` | `dappco.re/go/cache` | Request/response cache scoped to a named storage entry. | yes | no |
| `CachedRequest` | `type CachedRequest struct { URL string; Method string }` | `dappco.re/go/cache` | Key used for HTTP cache matching. | yes | no |
| `CachedResponse` | `type CachedResponse struct { Status int; StatusText string; Headers map[string]string; BodyPath string; CachedAt time.Time }` | `dappco.re/go/cache` | Stored HTTP response metadata. | yes | no |
| `New` | `func New(medium coreio.Medium, baseDir string, ttl time.Duration) (*Cache, error)` | `dappco.re/go/cache` | Creates a cache instance, applying default medium, base directory, and TTL when zero-valued inputs are provided. | yes | no |
| `(*Cache).Path` | `func (c *Cache) Path(key string) (string, error)` | `dappco.re/go/cache` | Returns the full path for a cache key and rejects path traversal. | yes | no |
| `(*Cache).Get` | `func (c *Cache) Get(key string, dest any) (bool, error)` | `dappco.re/go/cache` | Retrieves a cached item if it exists and has not expired. | yes | no |
| `(*Cache).Set` | `func (c *Cache) Set(key string, data any) error` | `dappco.re/go/cache` | Stores an item in the cache. | yes | no |
| `(*Cache).SetWithTTL` | `func (c *Cache) SetWithTTL(key string, data any, ttl time.Duration) error` | `dappco.re/go/cache` | Stores an item using a key-specific TTL. | yes | no |
| `(*Cache).SetBinary` | `func (c *Cache) SetBinary(key string, data []byte, contentType string) error` | `dappco.re/go/cache` | Stores a binary payload with JSON metadata. | yes | no |
| `(*Cache).SetBinaryWithTTL` | `func (c *Cache) SetBinaryWithTTL(key string, data []byte, contentType string, ttl time.Duration) error` | `dappco.re/go/cache` | Stores a binary payload using a key-specific TTL. | yes | no |
| `(*Cache).GetBinary` | `func (c *Cache) GetBinary(key string) ([]byte, bool, error)` | `dappco.re/go/cache` | Retrieves a binary payload if it exists and has not expired. | yes | no |
| `(*Cache).Delete` | `func (c *Cache) Delete(key string) error` | `dappco.re/go/cache` | Removes an item from the cache. | yes | no |
| `(*Cache).DeleteMany` | `func (c *Cache) DeleteMany(keys ...string) error` | `dappco.re/go/cache` | Removes several items from the cache in one call. | yes | no |
| `(*Cache).Clear` | `func (c *Cache) Clear() error` | `dappco.re/go/cache` | Removes all cached items. | yes | no |
| `(*Cache).Age` | `func (c *Cache) Age(key string) time.Duration` | `dappco.re/go/cache` | Returns how old a cached item is, or `-1` if it is not cached. | yes | no |
| `(*Cache).OnInvalidate` | `func (c *Cache) OnInvalidate(trigger string, fn InvalidateFunc)` | `dappco.re/go/cache` | Registers a cache invalidation callback. | yes | no |
| `(*Cache).Invalidate` | `func (c *Cache) Invalidate(trigger string) (int, error)` | `dappco.re/go/cache` | Runs invalidation callbacks and deletes matching entries. | yes | no |
| `(*Cache).Scoped` | `func (c *Cache) Scoped(origin string) *ScopedCache` | `dappco.re/go/cache` | Returns a namespaced cache view for an origin. | yes | no |
| `(*Cache).ClearScope` | `func (c *Cache) ClearScope(origin string) error` | `dappco.re/go/cache` | Removes all entries within an origin scope. | yes | no |
| `ScopedCache` | `type ScopedCache struct { /* unexported fields */ }` | `dappco.re/go/cache` | Origin-scoped cache wrapper. | yes | no |
| `(*ScopedCache).Path` | `func (c *ScopedCache) Path(key string) (string, error)` | `dappco.re/go/cache` | Resolves a scoped cache key to a storage path. | yes | no |
| `(*ScopedCache).Get` | `func (c *ScopedCache) Get(key string, dest any) (bool, error)` | `dappco.re/go/cache` | Retrieves a scoped entry. | yes | no |
| `(*ScopedCache).Set` | `func (c *ScopedCache) Set(key string, value any) error` | `dappco.re/go/cache` | Stores a scoped entry. | yes | no |
| `(*ScopedCache).SetWithTTL` | `func (c *ScopedCache) SetWithTTL(key string, value any, ttl time.Duration) error` | `dappco.re/go/cache` | Stores a scoped entry using a key-specific TTL. | yes | no |
| `(*ScopedCache).SetBinary` | `func (c *ScopedCache) SetBinary(key string, data []byte, contentType string) error` | `dappco.re/go/cache` | Stores a scoped binary payload. | yes | no |
| `(*ScopedCache).SetBinaryWithTTL` | `func (c *ScopedCache) SetBinaryWithTTL(key string, data []byte, contentType string, ttl time.Duration) error` | `dappco.re/go/cache` | Stores a scoped binary payload using a key-specific TTL. | yes | no |
| `(*ScopedCache).GetBinary` | `func (c *ScopedCache) GetBinary(key string) ([]byte, bool, error)` | `dappco.re/go/cache` | Retrieves a scoped binary payload. | yes | no |
| `(*ScopedCache).Delete` | `func (c *ScopedCache) Delete(key string) error` | `dappco.re/go/cache` | Removes a scoped entry. | yes | no |
| `(*ScopedCache).DeleteMany` | `func (c *ScopedCache) DeleteMany(keys ...string) error` | `dappco.re/go/cache` | Removes several scoped entries in one call. | yes | no |
| `(*ScopedCache).Clear` | `func (c *ScopedCache) Clear() error` | `dappco.re/go/cache` | Removes all entries in the scope. | yes | no |
| `(*ScopedCache).OnInvalidate` | `func (c *ScopedCache) OnInvalidate(trigger string, fn InvalidateFunc)` | `dappco.re/go/cache` | Registers a scoped invalidation callback. | yes | no |
| `(*ScopedCache).Invalidate` | `func (c *ScopedCache) Invalidate(trigger string) (int, error)` | `dappco.re/go/cache` | Runs invalidation callbacks for the scope. | yes | no |
| `(*ScopedCache).Age` | `func (c *ScopedCache) Age(key string) time.Duration` | `dappco.re/go/cache` | Returns scoped entry age, or `-1` if missing. | yes | no |
| `NewCacheStorage` | `func NewCacheStorage(medium coreio.Medium, baseDir string) (*CacheStorage, error)` | `dappco.re/go/cache` | Creates a named HTTP cache storage container. | yes | no |
| `(*CacheStorage).Open` | `func (cs *CacheStorage) Open(name string) (*HTTPCache, error)` | `dappco.re/go/cache` | Opens or creates a named HTTP cache. | yes | no |
| `(*CacheStorage).Delete` | `func (cs *CacheStorage) Delete(name string) error` | `dappco.re/go/cache` | Removes a named HTTP cache and its contents. | yes | no |
| `(*CacheStorage).Keys` | `func (cs *CacheStorage) Keys() ([]string, error)` | `dappco.re/go/cache` | Lists all named HTTP caches. | yes | no |
| `(*CacheStorage).Close` | `func (cs *CacheStorage) Close() error` | `dappco.re/go/cache` | Releases storage resources for compatibility. | yes | no |
| `(*HTTPCache).Match` | `func (hc *HTTPCache) Match(req CachedRequest) (*CachedResponse, error)` | `dappco.re/go/cache` | Finds a cached HTTP response by request. | yes | no |
| `(*HTTPCache).Put` | `func (hc *HTTPCache) Put(req CachedRequest, resp CachedResponse, body []byte) error` | `dappco.re/go/cache` | Stores a cached HTTP response and its body. | yes | no |
| `(*HTTPCache).ReadBody` | `func (hc *HTTPCache) ReadBody(resp *CachedResponse) ([]byte, error)` | `dappco.re/go/cache` | Reads a cached HTTP response body. | yes | no |
| `(*HTTPCache).Delete` | `func (hc *HTTPCache) Delete(req CachedRequest) error` | `dappco.re/go/cache` | Removes a cached HTTP request/response pair. | yes | no |
| `(*HTTPCache).Keys` | `func (hc *HTTPCache) Keys() ([]string, error)` | `dappco.re/go/cache` | Lists all cached request URLs. | yes | no |
| `GitHubReposKey` | `func GitHubReposKey(org string) string` | `dappco.re/go/cache` | Returns the cache key for an organization's repo list. | yes | no |
| `GitHubRepoKey` | `func GitHubRepoKey(org, repo string) string` | `dappco.re/go/cache` | Returns the cache key for a specific repo's metadata. | yes | no |
