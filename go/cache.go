// SPDX-License-Identifier: EUPL-1.2

// Package cache provides a storage-agnostic, JSON-based cache backed by any io.Medium.
package cache

import (
	"io/fs"
	"slices"
	"sync"
	"time"

	core "dappco.re/go"
	coreio "dappco.re/go/io"
)

// DefaultTTL is the default cache expiry time.
const DefaultTTL = time.Hour

const (
	maxCacheKeyBytes            = 4096
	maxCachePatternBytes        = 4096
	maxCacheNameBytes           = 255
	maxCachedRequestURLBytes    = 8192
	maxCachedRequestMethodBytes = 32
	maxCachedStatusTextBytes    = 1024
	maxCachedHeaderNameBytes    = 256
	maxCachedHeaderValueBytes   = 8192
	maxCachedHeaderCount        = 128
)

const (
	cacheStorageDirName = "cache-storage"
	responsesDirName    = "responses"
	responsesPathPrefix = responsesDirName + "/"

	opCacheNew                              = "cache.New"
	opCachePath                             = "cache.Path"
	opCacheGet                              = "cache.Get"
	opCacheSet                              = "cache.Set"
	opCacheSetInternal                      = "cache.set"
	opCacheRemoveEntryFiles                 = "cache.removeEntryFiles"
	opCacheSetBinary                        = "cache.setBinary"
	opCacheGetBinary                        = "cache.GetBinary"
	opCacheValidateKey                      = "cache.validateKey"
	opCacheValidatePattern                  = "cache.validatePattern"
	opCacheValidateResponseBodyPath         = "cache.validateResponseBodyPath"
	opCacheStorageOpen                      = "cache.CacheStorage.Open"
	opCacheStorageDelete                    = "cache.CacheStorage.Delete"
	opCacheRawBase64URLDecode               = "cache.rawBase64URLDecode"
	opHTTPCacheReadResponseRecord           = "cache.HTTPCache.readResponseRecord"
	opHTTPCachePut                          = "cache.HTTPCache.Put"
	opHTTPCacheReadBody                     = "cache.HTTPCache.ReadBody"
	opHTTPCacheValidateCachedResponseRecord = "cache.HTTPCache.validateCachedResponseRecord"
	opHTTPCacheValidateCachedRequest        = "cache.HTTPCache.validateCachedRequest"
	opHTTPCacheValidateCachedResponse       = "cache.HTTPCache.validateCachedResponse"
	opHTTPCacheDelete                       = "cache.HTTPCache.Delete"

	msgScopedCacheNil                = "scoped cache is nil"
	msgInvalidCacheName              = "invalid cache name"
	msgInvalidCachedRequest          = "invalid cached request"
	msgFailedUnmarshalCachedResponse = "failed to unmarshal cached response"
)

// Cache stores JSON-encoded entries in a Medium-backed cache rooted at baseDir.
type Cache struct {
	medium       coreio.Medium
	baseDir      string
	cacheTTL     time.Duration
	invalidation map[string][]InvalidateFunc
	entryMu      sync.RWMutex
	runtime      *core.Core
}

// Entry is the serialized cache record written to the backing Medium.
type Entry struct {
	Data      any       `json:"data"`
	CachedAt  time.Time `json:"cached_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// BinaryMeta is the metadata for binary cache payloads.
type BinaryMeta struct {
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CachedAt    time.Time `json:"cached_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// InvalidateFunc returns glob patterns to delete when a registered trigger fires.
type InvalidateFunc func(trigger string) []string

type entryPathSet struct {
	jsonPath   string
	binaryPath string
}

type fileSnapshot struct {
	path    string
	existed bool
	content string
}

type snapshotRestore struct {
	snapshot fileSnapshot
	message  string
}

// New creates a cache with explicit storage, root directory, and TTL.
func New(medium coreio.Medium, baseDir string, cacheTTL time.Duration) core.Result {
	if medium == nil {
		medium = coreio.Local
	}

	if baseDir == "" {
		cwd := currentDir()
		if cwd == "" || cwd == "." {
			return failure(opCacheNew, "failed to resolve current working directory", nil)
		}
		baseDir = normalizePath(core.JoinPath(cwd, ".core", "cache"))
	} else {
		baseDir = absolutePath(baseDir)
	}

	if cacheTTL < 0 {
		return failure(opCacheNew, "ttl must be >= 0", nil)
	}
	if cacheTTL == 0 {
		cacheTTL = DefaultTTL
	}
	if err := medium.EnsureDir(baseDir); err != nil {
		return failure(opCacheNew, "failed to create cache directory", err)
	}

	return core.Ok(&Cache{
		medium:       medium,
		baseDir:      baseDir,
		cacheTTL:     cacheTTL,
		invalidation: make(map[string][]InvalidateFunc),
		runtime:      core.New(),
	})
}

// Path resolves the on-disk JSON path for a cache key.
func (cache *Cache) Path(key string) core.Result {
	if r := cache.ensureConfigured(opCachePath); !r.OK {
		return r
	}
	if r := ensureSafeKey(key); !r.OK {
		return r
	}

	baseDir := absolutePath(cache.baseDir)
	path := absolutePath(core.JoinPath(baseDir, key+".json"))
	pathPrefix := normalizePath(core.Concat(baseDir, pathSeparator()))
	if path != baseDir && !core.HasPrefix(path, pathPrefix) {
		return failure(opCachePath, "invalid cache key: path traversal attempt", nil)
	}
	if r := ensureNoSymlinkPath(baseDir, path); !r.OK {
		return failure(opCachePath, "invalid cache key: symlink escape attempt", resultCause(r).Value.(error))
	}
	return core.Ok(path)
}

func (cache *Cache) entryPaths(key string) core.Result {
	pathResult := cache.Path(key)
	if !pathResult.OK {
		return pathResult
	}
	jsonPath := pathResult.Value.(string)
	baseDir := absolutePath(cache.baseDir)
	return core.Ok(entryPathSet{
		jsonPath:   jsonPath,
		binaryPath: absolutePath(core.JoinPath(baseDir, key+".bin")),
	})
}

// Get unmarshals the cached item into dest if it exists and has not expired.
func (cache *Cache) Get(key string, dest any) core.Result {
	if r := cache.ensureReady(opCacheGet); !r.OK {
		return r
	}

	cache.entryMu.RLock()
	defer cache.entryMu.RUnlock()

	pathResult := cache.Path(key)
	if !pathResult.OK {
		return pathResult
	}

	dataStr, err := cache.medium.Read(pathResult.Value.(string))
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok(false)
		}
		return failure(opCacheGet, "failed to read cache file", err)
	}

	var entry Entry
	if r := core.JSONUnmarshalString(dataStr, &entry); !r.OK {
		return failure(opCacheGet, "failed to unmarshal cache entry", resultCause(r).Value.(error))
	}
	if time.Now().After(entry.ExpiresAt) {
		return core.Ok(false)
	}

	payload := core.JSONMarshal(entry.Data)
	if !payload.OK {
		return failure(opCacheGet, "failed to marshal cached data", resultCause(payload).Value.(error))
	}
	if r := core.JSONUnmarshal(payload.Value.([]byte), dest); !r.OK {
		return failure(opCacheGet, "failed to unmarshal cached data", resultCause(r).Value.(error))
	}
	return core.Ok(true)
}

// Set stores a value using the cache's default TTL.
func (cache *Cache) Set(key string, data any) core.Result {
	if r := cache.ensureReady(opCacheSet); !r.OK {
		return r
	}
	return cache.set(key, data, cache.defaultTTL(), true)
}

// SetWithTTL stores a value with an explicit TTL override.
func (cache *Cache) SetWithTTL(key string, data any, ttl time.Duration) core.Result {
	if r := cache.ensureReady("cache.SetWithTTL"); !r.OK {
		return r
	}
	return cache.set(key, data, ttl, false)
}

func (cache *Cache) set(key string, data any, ttl time.Duration, useDefaultTTL bool) core.Result {
	if r := cache.ensureReady(opCacheSetInternal); !r.OK {
		return r
	}

	cache.entryMu.Lock()
	defer cache.entryMu.Unlock()

	pathsResult := cache.entryPaths(key)
	if !pathsResult.OK {
		return pathsResult
	}
	paths := pathsResult.Value.(entryPathSet)

	snapshotResult := readFileSnapshot(cache.medium, paths.jsonPath)
	if !snapshotResult.OK {
		return failure(opCacheSetInternal, "failed to inspect existing cache entry", resultCause(snapshotResult).Value.(error))
	}

	if err := cache.medium.EnsureDir(core.PathDir(paths.jsonPath)); err != nil {
		return failure(opCacheSet, "failed to create directory", err)
	}
	if ttl < 0 {
		return failure(opCacheSetInternal, "cache ttl must be >= 0", nil)
	}
	if ttl == 0 && useDefaultTTL {
		ttl = cache.defaultTTL()
	}

	now := time.Now()
	entry := Entry{Data: data, CachedAt: now, ExpiresAt: now.Add(ttl)}
	entryJSON := marshalPrettyJSON(entry)
	if !entryJSON.OK {
		return failure(opCacheSet, "failed to marshal cache entry", resultCause(entryJSON).Value.(error))
	}

	if err := cache.medium.Write(paths.jsonPath, entryJSON.Value.(string)); err != nil {
		restoreResult := restoreFileSnapshot(cache.medium, snapshotResult.Value.(fileSnapshot))
		if !restoreResult.OK {
			return failure(opCacheSetInternal, "failed to restore cache file after write failure", core.ErrorJoin(err, resultCause(restoreResult).Value.(error)))
		}
		return failure(opCacheSetInternal, "failed to write cache file", err)
	}
	return core.Ok(nil)
}

// Delete removes one cached entry.
func (cache *Cache) Delete(key string) core.Result {
	if r := cache.ensureReady("cache.Delete"); !r.OK {
		return r
	}
	r := cache.removeEntryFiles(key)
	if !r.OK && core.Is(resultCause(r).Value.(error), fs.ErrNotExist) {
		return core.Ok(nil)
	}
	if !r.OK {
		return r
	}
	return core.Ok(nil)
}

func (cache *Cache) removeEntryFiles(key string) core.Result {
	if r := cache.ensureReady(opCacheRemoveEntryFiles); !r.OK {
		return r
	}

	cache.entryMu.Lock()
	defer cache.entryMu.Unlock()

	pathsResult := cache.entryPaths(key)
	if !pathsResult.OK {
		return pathsResult
	}
	paths := pathsResult.Value.(entryPathSet)

	removed := false
	if err := cache.medium.Delete(paths.jsonPath); err != nil {
		if !core.Is(err, fs.ErrNotExist) {
			return failure(opCacheRemoveEntryFiles, "failed to delete cache json file", err)
		}
	} else {
		removed = true
	}
	if err := cache.medium.Delete(paths.binaryPath); err != nil {
		if !core.Is(err, fs.ErrNotExist) {
			return failure(opCacheRemoveEntryFiles, "failed to delete cache binary file", err)
		}
	} else {
		removed = true
	}
	return core.Ok(removed)
}

// SetBinary stores raw bytes in a sidecar .bin file and metadata in JSON.
func (cache *Cache) SetBinary(key string, data []byte, contentType string) core.Result {
	if r := cache.ensureReady("cache.SetBinary"); !r.OK {
		return r
	}
	return cache.setBinary(key, data, contentType, cache.defaultTTL(), true)
}

// SetBinaryWithTTL stores raw bytes with an explicit TTL override.
func (cache *Cache) SetBinaryWithTTL(key string, data []byte, contentType string, ttl time.Duration) core.Result {
	if r := cache.ensureReady("cache.SetBinaryWithTTL"); !r.OK {
		return r
	}
	return cache.setBinary(key, data, contentType, ttl, false)
}

func (cache *Cache) setBinary(key string, data []byte, contentType string, ttl time.Duration, useDefaultTTL bool) core.Result {
	if r := cache.ensureReady(opCacheSetBinary); !r.OK {
		return r
	}

	cache.entryMu.Lock()
	defer cache.entryMu.Unlock()

	pathsResult := cache.entryPaths(key)
	if !pathsResult.OK {
		return pathsResult
	}
	paths := pathsResult.Value.(entryPathSet)

	snapshots := readBinarySnapshots(cache.medium, paths.jsonPath, paths.binaryPath)
	if !snapshots.OK {
		return snapshots
	}
	pair := snapshots.Value.([2]fileSnapshot)

	if ttl < 0 {
		return failure(opCacheSetBinary, "cache ttl must be >= 0", nil)
	}
	if ttl == 0 && useDefaultTTL {
		ttl = cache.defaultTTL()
	}
	if err := cache.medium.EnsureDir(core.PathDir(paths.jsonPath)); err != nil {
		return failure(opCacheSetBinary, "failed to create directory", err)
	}

	now := time.Now()
	metaJSON := marshalPrettyJSON(BinaryMeta{
		ContentType: contentType,
		Size:        int64(len(data)),
		CachedAt:    now,
		ExpiresAt:   now.Add(ttl),
	})
	if !metaJSON.OK {
		return failure(opCacheSetBinary, "failed to marshal binary metadata", resultCause(metaJSON).Value.(error))
	}

	r := writeFileWithRollback(cache.medium, paths.binaryPath, string(data), opCacheSetBinary, "failed to write binary payload",
		snapshotRestore{snapshot: pair[0], message: "failed to restore binary metadata after payload write failure"},
		snapshotRestore{snapshot: pair[1], message: "failed to restore binary payload after payload write failure"},
	)
	if !r.OK {
		return r
	}
	return writeFileWithRollback(cache.medium, paths.jsonPath, metaJSON.Value.(string), opCacheSetBinary, "failed to write binary metadata",
		snapshotRestore{snapshot: pair[1], message: "failed to restore binary payload after metadata write failure"},
		snapshotRestore{snapshot: pair[0], message: "failed to restore binary metadata after metadata write failure"},
	)
}

// GetBinary returns raw binary cache payload. Missing or expired entries return OK with nil Value.
func (cache *Cache) GetBinary(key string) core.Result {
	if r := cache.ensureReady(opCacheGetBinary); !r.OK {
		return r
	}

	cache.entryMu.RLock()
	defer cache.entryMu.RUnlock()

	pathsResult := cache.entryPaths(key)
	if !pathsResult.OK {
		return pathsResult
	}
	paths := pathsResult.Value.(entryPathSet)

	rawMeta, err := cache.medium.Read(paths.jsonPath)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok(nil)
		}
		return failure(opCacheGetBinary, "failed to read binary metadata", err)
	}

	var meta BinaryMeta
	if r := core.JSONUnmarshalString(rawMeta, &meta); !r.OK {
		return failure(opCacheGetBinary, "failed to unmarshal binary metadata", resultCause(r).Value.(error))
	}
	if time.Now().After(meta.ExpiresAt) {
		return core.Ok(nil)
	}

	body, err := cache.medium.Read(paths.binaryPath)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok(nil)
		}
		return failure(opCacheGetBinary, "failed to read binary data", err)
	}
	return core.Ok([]byte(body))
}

// DeleteMany removes several entries in one call. Missing keys are ignored.
func (cache *Cache) DeleteMany(keys ...string) core.Result {
	if r := cache.ensureReady("cache.DeleteMany"); !r.OK {
		return r
	}

	cache.entryMu.Lock()
	defer cache.entryMu.Unlock()

	resolved := make([]entryPathSet, 0, len(keys))
	for _, key := range keys {
		pathsResult := cache.entryPaths(key)
		if !pathsResult.OK {
			return pathsResult
		}
		resolved = append(resolved, pathsResult.Value.(entryPathSet))
	}
	for _, paths := range resolved {
		if err := cache.medium.Delete(paths.jsonPath); err != nil && !core.Is(err, fs.ErrNotExist) {
			return failure("cache.DeleteMany", "failed to delete cache json file", err)
		}
		if err := cache.medium.Delete(paths.binaryPath); err != nil && !core.Is(err, fs.ErrNotExist) {
			return failure("cache.DeleteMany", "failed to delete cache binary file", err)
		}
	}
	return core.Ok(nil)
}

func (cache *Cache) listJSONKeys() core.Result {
	r := cache.collectJSONKeys("")
	if !r.OK {
		return r
	}
	keys := r.Value.([]string)
	slices.Sort(keys)
	return core.Ok(keys)
}

func (cache *Cache) collectJSONKeys(prefix string) core.Result {
	listPath := cache.baseDir
	if prefix != "" {
		listPath = core.JoinPath(cache.baseDir, prefix)
	}

	entries, err := cache.medium.List(listPath)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok([]string{})
		}
		return failure("cache.collectJSONKeys", "failed to list cache directory", err)
	}

	var keys []string
	for _, entry := range entries {
		name := entry.Name()
		childPrefix := name
		if prefix != "" {
			childPrefix = core.JoinPath(prefix, name)
		}
		if entry.IsDir() {
			childKeys := cache.collectJSONKeys(childPrefix)
			if !childKeys.OK {
				return childKeys
			}
			keys = append(keys, childKeys.Value.([]string)...)
			continue
		}
		if core.HasSuffix(name, ".json") {
			keys = append(keys, core.TrimSuffix(childPrefix, ".json"))
		}
	}
	return core.Ok(keys)
}

func (cache *Cache) keysByPattern(pattern string) core.Result {
	if r := ensureSafePattern(pattern); !r.OK {
		return r
	}

	cache.entryMu.RLock()
	defer cache.entryMu.RUnlock()

	allKeys := cache.listJSONKeys()
	if !allKeys.OK {
		return allKeys
	}

	var matched []string
	for _, key := range allKeys.Value.([]string) {
		match := matchKeyPattern(pattern, key)
		if match.OK && match.Value.(bool) {
			matched = append(matched, key)
		}
		if !match.OK {
			return failure("cache.keysByPattern", "failed to match pattern", resultCause(match).Value.(error))
		}
	}
	return core.Ok(matched)
}

func (cache *Cache) clearScope(prefix string) core.Result {
	keysResult := cache.keysByPattern(prefix)
	if !keysResult.OK {
		return keysResult
	}
	descendantsResult := cache.keysByPattern(prefix + "/*")
	if !descendantsResult.OK {
		return descendantsResult
	}
	keys := append(keysResult.Value.([]string), descendantsResult.Value.([]string)...)
	for _, key := range keys {
		if r := cache.removeEntryFiles(key); !r.OK {
			return r
		}
	}
	return core.Ok(nil)
}

func matchKeyPattern(pattern, key string) core.Result {
	if !containsAnyGlob(pattern) {
		return core.Ok(pattern == key)
	}
	if core.HasSuffix(pattern, "/*") {
		prefix := core.TrimSuffix(pattern, "/*")
		if prefix == "" {
			return core.Ok(true)
		}
		return core.Ok(core.HasPrefix(key, prefix+"/"))
	}

	patternParts := core.Split(pattern, "/")
	keyParts := core.Split(key, "/")
	if len(patternParts) != len(keyParts) {
		return core.Ok(false)
	}
	for i, part := range patternParts {
		if !containsAnyGlob(part) {
			if part != keyParts[i] {
				return core.Ok(false)
			}
			continue
		}
		match := segmentMatch(part, keyParts[i])
		if !match.OK || !match.Value.(bool) {
			return match
		}
	}
	return core.Ok(true)
}

func containsAnyGlob(s string) bool {
	for _, r := range s {
		if r == '*' || r == '?' || r == '[' || r == ']' {
			return true
		}
	}
	return false
}

func segmentMatch(pattern, name string) core.Result {
	p, n := 0, 0
	starP, starN := -1, 0
	for n < len(name) {
		if p < len(pattern) && (pattern[p] == '?' || pattern[p] == name[n]) {
			p++
			n++
			continue
		}
		if p < len(pattern) && pattern[p] == '*' {
			starP = p
			starN = n
			p++
			continue
		}
		if starP != -1 {
			p = starP + 1
			starN++
			n = starN
			continue
		}
		return core.Ok(false)
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return core.Ok(p == len(pattern))
}

// OnInvalidate registers a trigger callback that returns patterns to delete.
func (cache *Cache) OnInvalidate(trigger string, fn InvalidateFunc) {
	if cache == nil || fn == nil {
		return
	}
	if r := cache.ensureReady("cache.OnInvalidate"); !r.OK {
		return
	}
	lock := cache.runtime.Lock("cache")
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if cache.invalidation == nil {
		cache.invalidation = make(map[string][]InvalidateFunc)
	}
	cache.invalidation[trigger] = append(cache.invalidation[trigger], fn)
}

// Invalidate executes trigger callbacks and deletes matching entries.
func (cache *Cache) Invalidate(trigger string) core.Result {
	if r := cache.ensureReady("cache.Invalidate"); !r.OK {
		return r
	}

	callbacks := cache.invalidationCallbacks(trigger)
	total := 0
	for _, callback := range callbacks {
		deleted := cache.invalidatePatterns(callback(trigger))
		if !deleted.OK {
			return deleted
		}
		total += deleted.Value.(int)
	}
	return core.Ok(total)
}

func (cache *Cache) invalidationCallbacks(trigger string) []InvalidateFunc {
	lock := cache.runtime.Lock("cache")
	lock.Mutex.RLock()
	callbacks := append([]InvalidateFunc(nil), cache.invalidation[trigger]...)
	lock.Mutex.RUnlock()
	return callbacks
}

func (cache *Cache) invalidatePatterns(patterns []string) core.Result {
	total := 0
	for _, pattern := range patterns {
		deleted := cache.invalidatePattern(pattern)
		if !deleted.OK {
			return deleted
		}
		total += deleted.Value.(int)
	}
	return core.Ok(total)
}

func (cache *Cache) invalidatePattern(pattern string) core.Result {
	if pattern == "" {
		return core.Ok(0)
	}
	matchesResult := cache.keysByPattern(pattern)
	if !matchesResult.OK {
		return matchesResult
	}

	total := 0
	for _, key := range matchesResult.Value.([]string) {
		removed := cache.removeEntryFiles(key)
		if !removed.OK {
			return removed
		}
		if removed.Value.(bool) {
			total++
		}
	}
	return core.Ok(total)
}

// Scoped returns a cache namespaced by origin hash.
func (cache *Cache) Scoped(origin string) *ScopedCache {
	if cache == nil {
		return nil
	}
	return &ScopedCache{parent: cache, prefix: scopePrefix(origin)}
}

// ClearScope removes cache entries for a scoped origin.
func (cache *Cache) ClearScope(origin string) core.Result {
	if r := cache.ensureReady("cache.ClearScope"); !r.OK {
		return r
	}
	prefix := scopePrefix(origin)
	if r := ensureSafeKey(prefix); !r.OK {
		return r
	}
	return cache.clearScope(prefix)
}

func (cache *Cache) defaultTTL() time.Duration {
	if cache.cacheTTL <= 0 {
		return DefaultTTL
	}
	return cache.cacheTTL
}

// ScopedCache namespaces cache operations under a hashed origin prefix.
type ScopedCache struct {
	parent *Cache
	prefix string
}

func scopePrefix(origin string) string {
	return "scope_" + core.SHA256Hex([]byte(origin))
}

func (scopedCache *ScopedCache) fullKey(key string) string {
	return scopedCache.prefix + "/" + key
}

// Scoped returns a cache namespaced by a different origin.
func (scopedCache *ScopedCache) Scoped(origin string) *ScopedCache {
	if scopedCache == nil || scopedCache.parent == nil {
		return nil
	}
	return scopedCache.parent.Scoped(origin)
}

// Path resolves the on-disk JSON path for a scoped key.
func (scopedCache *ScopedCache) Path(key string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.Path", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.Path(scopedCache.fullKey(key))
}

// Get unmarshals a scoped cached item into dest.
func (scopedCache *ScopedCache) Get(key string, dest any) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.Get", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.Get(scopedCache.fullKey(key), dest)
}

// Set stores a scoped value using the parent cache's default TTL.
func (scopedCache *ScopedCache) Set(key string, value any) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.Set", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.Set(scopedCache.fullKey(key), value)
}

// SetWithTTL stores a scoped value with an explicit TTL.
func (scopedCache *ScopedCache) SetWithTTL(key string, value any, ttl time.Duration) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.SetWithTTL", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.SetWithTTL(scopedCache.fullKey(key), value, ttl)
}

// SetBinary stores scoped raw bytes.
func (scopedCache *ScopedCache) SetBinary(key string, data []byte, contentType string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.SetBinary", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.SetBinary(scopedCache.fullKey(key), data, contentType)
}

// SetBinaryWithTTL stores scoped raw bytes with an explicit TTL.
func (scopedCache *ScopedCache) SetBinaryWithTTL(key string, data []byte, contentType string, ttl time.Duration) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.SetBinaryWithTTL", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.SetBinaryWithTTL(scopedCache.fullKey(key), data, contentType, ttl)
}

// GetBinary returns scoped raw bytes. Missing or expired entries return OK with nil Value.
func (scopedCache *ScopedCache) GetBinary(key string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.GetBinary", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.GetBinary(scopedCache.fullKey(key))
}

// Delete removes one scoped entry.
func (scopedCache *ScopedCache) Delete(key string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.Delete", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.Delete(scopedCache.fullKey(key))
}

// DeleteMany removes several scoped entries.
func (scopedCache *ScopedCache) DeleteMany(keys ...string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.DeleteMany", msgScopedCacheNil, nil)
	}
	full := make([]string, len(keys))
	for i, key := range keys {
		full[i] = scopedCache.fullKey(key)
	}
	return scopedCache.parent.DeleteMany(full...)
}

// Clear removes all entries in the scope.
func (scopedCache *ScopedCache) Clear() core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.Clear", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.clearScope(scopedCache.prefix)
}

// ClearScope removes cache entries for a scoped origin.
func (scopedCache *ScopedCache) ClearScope(origin string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.ClearScope", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.ClearScope(origin)
}

// OnInvalidate registers a scoped invalidation callback.
func (scopedCache *ScopedCache) OnInvalidate(trigger string, fn InvalidateFunc) {
	if scopedCache == nil || scopedCache.parent == nil || fn == nil {
		return
	}
	prefix := scopedCache.prefix
	scopedCache.parent.OnInvalidate(trigger, func(trigger string) []string {
		patterns := fn(trigger)
		if len(patterns) == 0 {
			return nil
		}
		scopedPatterns := make([]string, 0, len(patterns))
		for _, pattern := range patterns {
			if pattern != "" {
				scopedPatterns = append(scopedPatterns, scopePattern(prefix, pattern))
			}
		}
		return scopedPatterns
	})
}

// Invalidate executes scoped trigger callbacks.
func (scopedCache *ScopedCache) Invalidate(trigger string) core.Result {
	if scopedCache == nil || scopedCache.parent == nil {
		return failure("cache.Scoped.Invalidate", msgScopedCacheNil, nil)
	}
	return scopedCache.parent.Invalidate(trigger)
}

// Age reports how long ago key was cached, or -1 if it is missing or unreadable.
func (scopedCache *ScopedCache) Age(key string) time.Duration {
	if scopedCache == nil || scopedCache.parent == nil {
		return -1
	}
	return scopedCache.parent.Age(scopedCache.fullKey(key))
}

func scopePattern(prefix, pattern string) string {
	pattern = core.TrimPrefix(pattern, "/")
	if pattern == "" {
		return prefix
	}
	return prefix + "/" + pattern
}

// CacheStorage manages named caches for HTTP cache API emulation.
type CacheStorage struct {
	medium  coreio.Medium
	baseDir string
	caches  map[string]*HTTPCache
	runtime *core.Core
}

// NewCacheStorage creates a namespace container for HTTPCache instances.
func NewCacheStorage(medium coreio.Medium, baseDir string) core.Result {
	if medium == nil {
		medium = coreio.Local
	}
	if baseDir == "" {
		cwd := currentDir()
		if cwd == "" || cwd == "." {
			return failure("cache.NewCacheStorage", "failed to resolve current working directory", nil)
		}
		baseDir = normalizePath(core.JoinPath(cwd, ".core", cacheStorageDirName))
	} else {
		baseDir = absolutePath(baseDir)
	}
	if err := medium.EnsureDir(baseDir); err != nil {
		return failure("cache.NewCacheStorage", "failed to create cache storage directory", err)
	}
	return core.Ok(&CacheStorage{
		medium:  medium,
		baseDir: baseDir,
		caches:  make(map[string]*HTTPCache),
		runtime: core.New(),
	})
}

// Open retrieves a named HTTPCache, creating it on first use.
func (storage *CacheStorage) Open(name string) core.Result {
	if r := storage.ensureReady(opCacheStorageOpen); !r.OK {
		return r
	}
	if r := ensureSafeCacheName(opCacheStorageOpen, name); !r.OK {
		return r
	}

	lock := storage.runtime.Lock(cacheStorageDirName)
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if httpCache, ok := storage.caches[name]; ok {
		return core.Ok(httpCache)
	}

	cacheDir := core.JoinPath(storage.baseDir, name)
	if err := storage.medium.EnsureDir(cacheDir); err != nil {
		return failure(opCacheStorageOpen, "failed to create cache directory", err)
	}
	httpCache := &HTTPCache{name: name, medium: storage.medium, baseDir: cacheDir}
	storage.caches[name] = httpCache
	return core.Ok(httpCache)
}

// Delete removes a named HTTP cache and all entries.
func (storage *CacheStorage) Delete(name string) core.Result {
	if r := storage.ensureReady(opCacheStorageDelete); !r.OK {
		return r
	}
	if r := ensureSafeCacheName(opCacheStorageDelete, name); !r.OK {
		return r
	}

	lock := storage.runtime.Lock(cacheStorageDirName)
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if err := storage.medium.DeleteAll(core.JoinPath(storage.baseDir, name)); err != nil && !core.Is(err, fs.ErrNotExist) {
		return failure(opCacheStorageDelete, "failed to delete cache directory", err)
	}
	delete(storage.caches, name)
	return core.Ok(nil)
}

// Keys lists all named caches.
func (storage *CacheStorage) Keys() core.Result {
	if r := storage.ensureReady("cache.CacheStorage.Keys"); !r.OK {
		return r
	}

	lock := storage.runtime.Lock(cacheStorageDirName)
	lock.Mutex.RLock()
	names := make(map[string]struct{}, len(storage.caches))
	for name := range storage.caches {
		names[name] = struct{}{}
	}
	lock.Mutex.RUnlock()

	entries, err := storage.medium.List(storage.baseDir)
	if err != nil && !core.Is(err, fs.ErrNotExist) {
		return failure("cache.CacheStorage.Keys", "failed to list caches", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			names[entry.Name()] = struct{}{}
		}
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	slices.Sort(out)
	return core.Ok(out)
}

// Close releases storage resources for compatibility with long-lived workflows.
func (storage *CacheStorage) Close() core.Result {
	if storage == nil {
		return core.Ok(nil)
	}
	if storage.runtime == nil {
		storage.caches = make(map[string]*HTTPCache)
		return core.Ok(nil)
	}
	lock := storage.runtime.Lock(cacheStorageDirName)
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	storage.caches = make(map[string]*HTTPCache)
	return core.Ok(nil)
}

func (storage *CacheStorage) ensureReady(op string) core.Result {
	if storage == nil {
		return failure(op, "cache storage is nil", nil)
	}
	if storage.medium == nil {
		return failure(op, "cache storage medium is nil; construct via cache.NewCacheStorage", nil)
	}
	if storage.baseDir == "" {
		return failure(op, "cache storage base directory is empty; construct via cache.NewCacheStorage", nil)
	}
	if storage.runtime == nil {
		return failure(op, "cache storage runtime is nil; construct via cache.NewCacheStorage", nil)
	}
	lock := storage.runtime.Lock(cacheStorageDirName)
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if storage.caches == nil {
		storage.caches = make(map[string]*HTTPCache)
	}
	return core.Ok(nil)
}

// HTTPCache stores request/response pairs.
type HTTPCache struct {
	name    string
	medium  coreio.Medium
	baseDir string
}

func (httpCache *HTTPCache) ensureReady(op string) core.Result {
	if httpCache == nil {
		return failure(op, "http cache is nil", nil)
	}
	if httpCache.medium == nil {
		return failure(op, "http cache medium is nil; construct via cache.CacheStorage.Open", nil)
	}
	if httpCache.baseDir == "" {
		return failure(op, "http cache base directory is empty; construct via cache.CacheStorage.Open", nil)
	}
	return core.Ok(nil)
}

// CachedRequest identifies a request by URL and method.
type CachedRequest struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

// CachedResponse stores HTTP metadata for a cached response body.
type CachedResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"status_text"`
	Headers    map[string]string `json:"headers"`
	BodyPath   string            `json:"body_path"`
	CachedAt   time.Time         `json:"cached_at"`
}

type cachedResponseRecord struct {
	Request  CachedRequest  `json:"request"`
	Response CachedResponse `json:"response"`
}

func (httpCache *HTTPCache) storagePath(parts ...string) string {
	args := append([]string{httpCache.baseDir}, parts...)
	return core.JoinPath(args...)
}

func (httpCache *HTTPCache) requestKey(req CachedRequest) core.Result {
	return requestStorageKey(req)
}

func legacyRequestKey(req CachedRequest) string {
	return rawBase64URLEncode([]byte(req.Method + "\x00" + req.URL))
}

func rawBase64URLEncode(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	builder := core.NewBuilder()
	i := 0
	for ; i+3 <= len(data); i += 3 {
		n := uint(data[i])<<16 | uint(data[i+1])<<8 | uint(data[i+2])
		builder.WriteByte(alphabet[(n>>18)&0x3f])
		builder.WriteByte(alphabet[(n>>12)&0x3f])
		builder.WriteByte(alphabet[(n>>6)&0x3f])
		builder.WriteByte(alphabet[n&0x3f])
	}
	switch len(data) - i {
	case 1:
		n := uint(data[i]) << 16
		builder.WriteByte(alphabet[(n>>18)&0x3f])
		builder.WriteByte(alphabet[(n>>12)&0x3f])
	case 2:
		n := uint(data[i])<<16 | uint(data[i+1])<<8
		builder.WriteByte(alphabet[(n>>18)&0x3f])
		builder.WriteByte(alphabet[(n>>12)&0x3f])
		builder.WriteByte(alphabet[(n>>6)&0x3f])
	}
	return builder.String()
}

func rawBase64URLDecode(encoded string) core.Result {
	if core.Contains(encoded, "=") {
		return failure(opCacheRawBase64URLDecode, "raw URL base64 must not contain padding", nil)
	}
	if len(encoded)%4 == 1 {
		return failure(opCacheRawBase64URLDecode, "invalid raw URL base64 length", nil)
	}

	out := make([]byte, 0, len(encoded)*3/4)
	for i := 0; i < len(encoded); {
		remaining := len(encoded) - i
		chunkLen := min(remaining, 4)

		var values [4]byte
		for j := range chunkLen {
			value := rawBase64URLDecodeValue(encoded[i+j])
			if value < 0 {
				return failure(opCacheRawBase64URLDecode, "invalid raw URL base64 character", nil)
			}
			values[j] = byte(value)
		}

		out = append(out, values[0]<<2|values[1]>>4)
		if chunkLen >= 3 {
			out = append(out, values[1]<<4|values[2]>>2)
		}
		if chunkLen == 4 {
			out = append(out, values[2]<<6|values[3])
		}
		i += chunkLen
	}
	return core.Ok(out)
}

func rawBase64URLDecodeValue(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 26
	case c >= '0' && c <= '9':
		return int(c-'0') + 52
	case c == '-':
		return 62
	case c == '_':
		return 63
	default:
		return -1
	}
}

func decodeRequestKey(encoded string) core.Result {
	raw := rawBase64URLDecode(encoded)
	if !raw.OK {
		return failure("cache.decodeRequestKey", "invalid cached request key", resultCause(raw).Value.(error))
	}
	parts := core.SplitN(string(raw.Value.([]byte)), "\x00", 2)
	if len(parts) != 2 {
		return failure("cache.decodeRequestKey", "invalid cached request key payload", nil)
	}
	return core.Ok(CachedRequest{Method: parts[0], URL: parts[1]})
}

func (httpCache *HTTPCache) responseMetaPath(key string) string {
	return httpCache.storagePath(responsesDirName, key+".json")
}

func (httpCache *HTTPCache) responseBinaryPath(key string) string {
	return httpCache.storagePath(responsesDirName, key+".bin")
}

func (httpCache *HTTPCache) readResponseRecord(key string) core.Result {
	raw, err := httpCache.medium.Read(httpCache.responseMetaPath(key))
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok(nil)
		}
		return failure(opHTTPCacheReadResponseRecord, "failed to read cached response", err)
	}

	envelope := cachedResponseEnvelopeState(raw)
	if !envelope.OK {
		return envelope
	}
	state := envelope.Value.([2]bool)
	if state[0] || state[1] {
		return parseCachedResponseRecord(key, raw, state[0], state[1])
	}
	return parseLegacyCachedResponseRecord(key, raw)
}

func cachedResponseEnvelopeState(raw string) core.Result {
	var envelope map[string]any
	if r := core.JSONUnmarshalString(raw, &envelope); !r.OK {
		return failure(opHTTPCacheReadResponseRecord, msgFailedUnmarshalCachedResponse, resultCause(r).Value.(error))
	}
	_, hasRequest := envelope["request"]
	_, hasResponse := envelope["response"]
	return core.Ok([2]bool{hasRequest, hasResponse})
}

func parseCachedResponseRecord(key, raw string, hasRequest, hasResponse bool) core.Result {
	if !hasRequest || !hasResponse {
		return failure(opHTTPCacheReadResponseRecord, "cached response envelope is incomplete", nil)
	}

	var record cachedResponseRecord
	if r := core.JSONUnmarshalString(raw, &record); !r.OK {
		return failure(opHTTPCacheReadResponseRecord, msgFailedUnmarshalCachedResponse, resultCause(r).Value.(error))
	}
	if r := validateCachedResponseRecord(key, &record); !r.OK {
		return r
	}
	return core.Ok(&record)
}

func parseLegacyCachedResponseRecord(key, raw string) core.Result {
	var response CachedResponse
	if r := core.JSONUnmarshalString(raw, &response); !r.OK {
		return failure(opHTTPCacheReadResponseRecord, msgFailedUnmarshalCachedResponse, resultCause(r).Value.(error))
	}

	req := decodeRequestKey(key)
	if !req.OK {
		return req
	}
	record := cachedResponseRecord{Request: req.Value.(CachedRequest), Response: response}
	if r := validateCachedResponseRecord(key, &record); !r.OK {
		return r
	}
	return core.Ok(&record)
}

// Match finds a cached response for request. Missing entries return OK with nil Value.
func (httpCache *HTTPCache) Match(req CachedRequest) core.Result {
	if r := httpCache.ensureReady("cache.HTTPCache.Match"); !r.OK {
		return r
	}
	if r := validateCachedRequest(req); !r.OK {
		return failure("cache.HTTPCache.Match", msgInvalidCachedRequest, resultCause(r).Value.(error))
	}

	key := httpCache.requestKey(req)
	if !key.OK {
		return key
	}
	record := httpCache.readResponseRecord(key.Value.(string))
	if !record.OK {
		return record
	}
	if record.Value == nil {
		record = httpCache.readResponseRecord(legacyRequestKey(req))
	}
	if !record.OK || record.Value == nil {
		return record
	}
	return core.Ok(&record.Value.(*cachedResponseRecord).Response)
}

// Put stores a request/response pair and its body.
func (httpCache *HTTPCache) Put(req CachedRequest, resp CachedResponse, body []byte) core.Result {
	if r := httpCache.ensureReady(opHTTPCachePut); !r.OK {
		return r
	}
	key := httpCache.requestKey(req)
	if !key.OK {
		return key
	}
	if r := validateCachedRequest(req); !r.OK {
		return failure(opHTTPCachePut, msgInvalidCachedRequest, resultCause(r).Value.(error))
	}
	resp.BodyPath = core.JoinPath(responsesDirName, key.Value.(string)+".bin")
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}
	if r := validateCachedResponse(resp); !r.OK {
		return failure(opHTTPCachePut, "invalid cached response", resultCause(r).Value.(error))
	}
	if err := httpCache.medium.EnsureDir(httpCache.storagePath(responsesDirName)); err != nil {
		return failure(opHTTPCachePut, "failed to create response directory", err)
	}

	metaPath := httpCache.responseMetaPath(key.Value.(string))
	binaryPath := httpCache.responseBinaryPath(key.Value.(string))
	snapshots := readCachedResponseSnapshots(httpCache.medium, metaPath, binaryPath)
	if !snapshots.OK {
		return snapshots
	}
	pair := snapshots.Value.([2]fileSnapshot)

	resp.CachedAt = time.Now()
	meta := marshalPrettyJSON(cachedResponseRecord{Request: req, Response: resp})
	if !meta.OK {
		return failure(opHTTPCachePut, "failed to marshal cached response", resultCause(meta).Value.(error))
	}

	r := writeFileWithRollback(httpCache.medium, binaryPath, string(body), opHTTPCachePut, "failed to write cached response body",
		snapshotRestore{snapshot: pair[0], message: "failed to restore response metadata after body write failure"},
		snapshotRestore{snapshot: pair[1], message: "failed to restore response body after body write failure"},
	)
	if !r.OK {
		return r
	}
	return writeFileWithRollback(httpCache.medium, metaPath, meta.Value.(string), opHTTPCachePut, "failed to write cached response metadata",
		snapshotRestore{snapshot: pair[1], message: "failed to restore response body after metadata write failure"},
		snapshotRestore{snapshot: pair[0], message: "failed to restore response metadata after metadata write failure"},
	)
}

// ReadBody returns the response body bytes.
func (httpCache *HTTPCache) ReadBody(resp *CachedResponse) core.Result {
	if r := httpCache.ensureReady(opHTTPCacheReadBody); !r.OK {
		return r
	}
	if resp == nil {
		return failure(opHTTPCacheReadBody, "response is nil", nil)
	}
	if resp.BodyPath == "" {
		return failure(opHTTPCacheReadBody, "response has empty body path", nil)
	}
	if r := ensureSafeResponseBodyPath(resp.BodyPath); !r.OK {
		return failure(opHTTPCacheReadBody, "invalid response body path", resultCause(r).Value.(error))
	}
	body, err := httpCache.medium.Read(httpCache.storagePath(resp.BodyPath))
	if err != nil {
		return failure(opHTTPCacheReadBody, "failed to read response body", err)
	}
	return core.Ok([]byte(body))
}

func validateCachedResponseRecord(key string, record *cachedResponseRecord) core.Result {
	if record == nil {
		return failure(opHTTPCacheValidateCachedResponseRecord, "cached response record is nil", nil)
	}
	if r := validateCachedRequest(record.Request); !r.OK {
		return failure(opHTTPCacheValidateCachedResponseRecord, msgInvalidCachedRequest, resultCause(r).Value.(error))
	}

	expectedKey := requestStorageKey(record.Request)
	if !expectedKey.OK {
		return expectedKey
	}
	legacyKey := legacyRequestKey(record.Request)
	if key != expectedKey.Value.(string) && key != legacyKey {
		return failure(opHTTPCacheValidateCachedResponseRecord, "cached request metadata does not match cache key", nil)
	}
	if r := validateCachedResponse(record.Response); !r.OK {
		return r
	}
	expectedBodyPaths := []string{
		core.JoinPath(responsesDirName, expectedKey.Value.(string)+".bin"),
		core.JoinPath(responsesDirName, legacyKey+".bin"),
	}
	if !slices.Contains(expectedBodyPaths, record.Response.BodyPath) {
		return failure(opHTTPCacheValidateCachedResponseRecord, "cached response body path does not match cache key", nil)
	}
	return core.Ok(nil)
}

func requestStorageKey(req CachedRequest) core.Result {
	if r := validateCachedRequest(req); !r.OK {
		return failure("cache.HTTPCache.requestStorageKey", msgInvalidCachedRequest, resultCause(r).Value.(error))
	}
	return core.Ok(core.SHA256Hex([]byte(req.Method + "\x00" + req.URL)))
}

func validateCachedRequest(req CachedRequest) core.Result {
	if core.Trim(req.URL) == "" || core.Trim(req.Method) == "" {
		return failure(opHTTPCacheValidateCachedRequest, "request URL and method are required", nil)
	}
	if len(req.URL) > maxCachedRequestURLBytes {
		return failure(opHTTPCacheValidateCachedRequest, "request URL is too long", nil)
	}
	if len(req.Method) > maxCachedRequestMethodBytes {
		return failure(opHTTPCacheValidateCachedRequest, "request method is too long", nil)
	}
	if hasHTTPDangerousBytes(req.URL) || hasHTTPDangerousBytes(req.Method) {
		return failure(opHTTPCacheValidateCachedRequest, "request contains control characters", nil)
	}
	if !isHTTPToken(req.Method) {
		return failure(opHTTPCacheValidateCachedRequest, "invalid HTTP method", nil)
	}
	return core.Ok(nil)
}

func validateCachedResponse(resp CachedResponse) core.Result {
	if resp.Status < 100 || resp.Status > 599 {
		return failure(opHTTPCacheValidateCachedResponse, "invalid HTTP status", nil)
	}
	if hasHTTPDangerousBytes(resp.StatusText) {
		return failure(opHTTPCacheValidateCachedResponse, "invalid HTTP status text", nil)
	}
	if len(resp.StatusText) > maxCachedStatusTextBytes {
		return failure(opHTTPCacheValidateCachedResponse, "HTTP status text is too long", nil)
	}
	if r := ensureSafeResponseBodyPath(resp.BodyPath); !r.OK {
		return failure(opHTTPCacheValidateCachedResponse, "invalid response body path", resultCause(r).Value.(error))
	}
	if len(resp.Headers) > maxCachedHeaderCount {
		return failure(opHTTPCacheValidateCachedResponse, "too many response headers", nil)
	}
	for name, value := range resp.Headers {
		if len(name) > maxCachedHeaderNameBytes {
			return failure(opHTTPCacheValidateCachedResponse, "response header name is too long", nil)
		}
		if len(value) > maxCachedHeaderValueBytes {
			return failure(opHTTPCacheValidateCachedResponse, "response header value is too long", nil)
		}
		if r := validateHTTPHeaderName(name); !r.OK {
			return failure(opHTTPCacheValidateCachedResponse, "invalid response header name", resultCause(r).Value.(error))
		}
		if hasHTTPDangerousBytes(value) {
			return failure(opHTTPCacheValidateCachedResponse, "invalid response header value", nil)
		}
	}
	return core.Ok(nil)
}

func validateHTTPHeaderName(name string) core.Result {
	if name == "" {
		return failure("cache.HTTPCache.validateHTTPHeaderName", "header name is empty", nil)
	}
	if !isHTTPToken(name) {
		return failure("cache.HTTPCache.validateHTTPHeaderName", "invalid header name", nil)
	}
	return core.Ok(nil)
}

func hasHTTPDangerousBytes(s string) bool {
	return hasDangerousBytes(s)
}

func isHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '!' || c == '#' || c == '$' || c == '%' || c == '&' || c == '\'' || c == '*' || c == '+' || c == '-' || c == '.' || c == '^' || c == '_' || c == '`' || c == '|' || c == '~':
		default:
			return false
		}
	}
	return true
}

// Delete removes a cached request/response pair.
func (httpCache *HTTPCache) Delete(req CachedRequest) core.Result {
	if r := httpCache.ensureReady(opHTTPCacheDelete); !r.OK {
		return r
	}
	if r := validateCachedRequest(req); !r.OK {
		return failure(opHTTPCacheDelete, msgInvalidCachedRequest, resultCause(r).Value.(error))
	}

	key := httpCache.requestKey(req)
	if !key.OK {
		return key
	}
	keys := []string{key.Value.(string)}
	legacyKey := legacyRequestKey(req)
	if legacyKey != key.Value.(string) {
		keys = append(keys, legacyKey)
	}
	for _, current := range keys {
		if err := httpCache.medium.Delete(httpCache.responseMetaPath(current)); err != nil && !core.Is(err, fs.ErrNotExist) {
			return failure(opHTTPCacheDelete, "failed to delete cached response metadata", err)
		}
		if err := httpCache.medium.Delete(httpCache.responseBinaryPath(current)); err != nil && !core.Is(err, fs.ErrNotExist) {
			return failure(opHTTPCacheDelete, "failed to delete cached response body", err)
		}
	}
	return core.Ok(nil)
}

// Keys returns all cached request URLs.
func (httpCache *HTTPCache) Keys() core.Result {
	if r := httpCache.ensureReady("cache.HTTPCache.Keys"); !r.OK {
		return r
	}

	entries, err := httpCache.medium.List(httpCache.storagePath(responsesDirName))
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok([]string{})
		}
		return failure("cache.HTTPCache.Keys", "failed to list response entries", err)
	}

	seen := make(map[string]struct{})
	var urls []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !core.HasSuffix(name, ".json") {
			continue
		}
		record := httpCache.readResponseRecord(core.TrimSuffix(name, ".json"))
		if !record.OK || record.Value == nil {
			continue
		}
		url := record.Value.(*cachedResponseRecord).Request.URL
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	slices.Sort(urls)
	return core.Ok(urls)
}

func readBinarySnapshots(medium coreio.Medium, jsonPath, binaryPath string) core.Result {
	jsonSnapshot := readSnapshot(medium, jsonPath, opCacheSetBinary, "failed to inspect existing binary metadata")
	if !jsonSnapshot.OK {
		return jsonSnapshot
	}
	binarySnapshot := readSnapshot(medium, binaryPath, opCacheSetBinary, "failed to inspect existing binary payload")
	if !binarySnapshot.OK {
		return binarySnapshot
	}
	return core.Ok([2]fileSnapshot{jsonSnapshot.Value.(fileSnapshot), binarySnapshot.Value.(fileSnapshot)})
}

func readCachedResponseSnapshots(medium coreio.Medium, metaPath, binaryPath string) core.Result {
	metaSnapshot := readSnapshot(medium, metaPath, opHTTPCachePut, "failed to inspect existing cached response metadata")
	if !metaSnapshot.OK {
		return metaSnapshot
	}
	binarySnapshot := readSnapshot(medium, binaryPath, opHTTPCachePut, "failed to inspect existing cached response body")
	if !binarySnapshot.OK {
		return binarySnapshot
	}
	return core.Ok([2]fileSnapshot{metaSnapshot.Value.(fileSnapshot), binarySnapshot.Value.(fileSnapshot)})
}

func readSnapshot(medium coreio.Medium, path, op, message string) core.Result {
	snapshot := readFileSnapshot(medium, path)
	if !snapshot.OK {
		return failure(op, message, resultCause(snapshot).Value.(error))
	}
	return snapshot
}

func readFileSnapshot(medium coreio.Medium, path string) core.Result {
	content, err := medium.Read(path)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return core.Ok(fileSnapshot{path: path})
		}
		return core.Fail(err)
	}
	return core.Ok(fileSnapshot{path: path, existed: true, content: content})
}

func writeFileWithRollback(medium coreio.Medium, path, content, op, message string, restores ...snapshotRestore) core.Result {
	if err := medium.Write(path, content); err != nil {
		restoreResult := restoreSnapshotsAfterFailure(medium, err, op, restores...)
		if !restoreResult.OK {
			return restoreResult
		}
		return failure(op, message, err)
	}
	return core.Ok(nil)
}

func restoreSnapshotsAfterFailure(medium coreio.Medium, cause error, op string, restores ...snapshotRestore) core.Result {
	for _, restore := range restores {
		r := restoreFileSnapshot(medium, restore.snapshot)
		if !r.OK {
			return failure(op, restore.message, core.ErrorJoin(cause, resultCause(r).Value.(error)))
		}
	}
	return core.Ok(nil)
}

func restoreFileSnapshot(medium coreio.Medium, snapshot fileSnapshot) core.Result {
	if snapshot.path == "" {
		return core.Ok(nil)
	}
	if !snapshot.existed {
		if err := medium.Delete(snapshot.path); err != nil && !core.Is(err, fs.ErrNotExist) {
			return core.Fail(err)
		}
		return core.Ok(nil)
	}
	if err := medium.Write(snapshot.path, snapshot.content); err != nil {
		return core.Fail(err)
	}
	return core.Ok(nil)
}

// Clear removes all cached items under the cache base directory.
func (c *Cache) Clear() core.Result {
	if r := c.ensureReady("cache.Clear"); !r.OK {
		return r
	}
	if err := c.medium.DeleteAll(c.baseDir); err != nil {
		return failure("cache.Clear", "failed to clear cache", err)
	}
	return core.Ok(nil)
}

// Age reports how long ago key was cached, or -1 if it is missing or unreadable.
func (c *Cache) Age(key string) time.Duration {
	if r := c.ensureReady("cache.Age"); !r.OK {
		return -1
	}
	path := c.Path(key)
	if !path.OK {
		return -1
	}
	dataStr, err := c.medium.Read(path.Value.(string))
	if err != nil {
		return -1
	}
	var entry Entry
	if r := core.JSONUnmarshalString(dataStr, &entry); !r.OK {
		return -1
	}
	return time.Since(entry.CachedAt)
}

// GitHubReposKey returns the cache key used for an organisation's repo list.
func GitHubReposKey(org string) string {
	return core.JoinPath("github", encodePathSegment(org), "repos")
}

// GitHubRepoKey returns the cache key used for a repository metadata entry.
func GitHubRepoKey(org, repo string) string {
	return core.JoinPath("github", encodePathSegment(org), encodePathSegment(repo), "meta")
}

func encodePathSegment(segment string) string {
	return core.URLPathEscape(segment)
}

func marshalPrettyJSON(value any) core.Result {
	result := core.JSONMarshalIndent(value, "", "  ")
	if !result.OK {
		return result
	}
	return core.Ok(string(result.Value.([]byte)))
}

func ensureSafeKey(key string) core.Result {
	if key == "" {
		return failure(opCacheValidateKey, "invalid empty key", nil)
	}
	if len(key) > maxCacheKeyBytes {
		return failure(opCacheValidateKey, "invalid key: too long", nil)
	}
	if core.Contains(key, "\\") {
		return failure(opCacheValidateKey, "invalid key: contains path separators", nil)
	}
	if hasPathDangerousBytes(key) {
		return failure(opCacheValidateKey, "invalid key: contains control bytes", nil)
	}
	for _, part := range core.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return failure(opCacheValidateKey, "invalid key: path traversal attempt", nil)
		}
	}
	return core.Ok(nil)
}

func ensureSafePattern(pattern string) core.Result {
	if pattern == "" {
		return failure(opCacheValidatePattern, "invalid empty pattern", nil)
	}
	if len(pattern) > maxCachePatternBytes {
		return failure(opCacheValidatePattern, "invalid pattern: too long", nil)
	}
	if core.Contains(pattern, "\\") || hasPathDangerousBytes(pattern) {
		return failure(opCacheValidatePattern, "invalid pattern: contains control bytes", nil)
	}
	return core.Ok(nil)
}

func ensureNoSymlinkPath(baseDir, path string) core.Result {
	if r := rejectSymlink(baseDir); !r.OK {
		return r
	}
	if path == baseDir {
		return core.Ok(nil)
	}

	rel := core.TrimPrefix(path, normalizePath(core.Concat(baseDir, pathSeparator())))
	if rel == path {
		return core.Ok(nil)
	}
	current := baseDir
	for _, part := range core.Split(rel, pathSeparator()) {
		if part == "" {
			continue
		}
		current = core.JoinPath(current, part)
		if r := rejectSymlink(current); !r.OK {
			return r
		}
	}
	return core.Ok(nil)
}

func rejectSymlink(path string) core.Result {
	result := core.Lstat(path)
	if !result.OK {
		if core.Is(resultCause(result).Value.(error), fs.ErrNotExist) {
			return core.Ok(nil)
		}
		return result
	}
	info := result.Value.(fs.FileInfo)
	if info.Mode()&fs.ModeSymlink != 0 {
		return failure("cache.validatePath", "path contains symlink", nil)
	}
	return core.Ok(nil)
}

func hasPathDangerousBytes(s string) bool {
	return hasDangerousBytes(s)
}

func hasDangerousBytes(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func ensureSafeResponseBodyPath(path string) core.Result {
	if path == "" {
		return failure(opCacheValidateResponseBodyPath, "invalid empty body path", nil)
	}
	if len(path) > maxCacheKeyBytes {
		return failure(opCacheValidateResponseBodyPath, "invalid body path: too long", nil)
	}
	if core.PathIsAbs(path) {
		return failure(opCacheValidateResponseBodyPath, "invalid body path: absolute paths are not allowed", nil)
	}
	if core.Contains(path, "\\") || hasPathDangerousBytes(path) {
		return failure(opCacheValidateResponseBodyPath, "invalid body path: contains control bytes", nil)
	}

	normalized := normalizePath(path)
	if !core.HasPrefix(normalized, responsesPathPrefix) || !core.HasSuffix(normalized, ".bin") {
		return failure(opCacheValidateResponseBodyPath, "invalid body path: expected responses/<key>.bin", nil)
	}

	rel := core.TrimPrefix(normalized, responsesPathPrefix)
	rel = core.TrimSuffix(rel, ".bin")
	if rel == "" {
		return failure(opCacheValidateResponseBodyPath, "invalid body path", nil)
	}
	for _, segment := range core.Split(rel, "/") {
		if r := ensureSafeKey(segment); !r.OK {
			return failure(opCacheValidateResponseBodyPath, "invalid body path", resultCause(r).Value.(error))
		}
	}
	return core.Ok(nil)
}

func ensureSafeCacheName(op, name string) core.Result {
	if name == "" {
		return failure(op, "cache name is empty", nil)
	}
	if len(name) > maxCacheNameBytes {
		return failure(op, "invalid cache name: too long", nil)
	}
	if core.Contains(name, "/") || core.Contains(name, `\`) {
		return failure(op, msgInvalidCacheName, nil)
	}
	if hasPathDangerousBytes(name) {
		return failure(op, msgInvalidCacheName, nil)
	}
	if name == "." || name == ".." {
		return failure(op, msgInvalidCacheName, nil)
	}
	return core.Ok(nil)
}

func pathSeparator() string {
	if ds := core.Env("DS"); ds != "" {
		return ds
	}
	return "/"
}

func normalizePath(path string) string {
	ds := pathSeparator()
	normalized := core.Replace(path, "\\", ds)
	if ds != "/" {
		normalized = core.Replace(normalized, "/", ds)
	}
	return core.CleanPath(normalized, ds)
}

func absolutePath(path string) string {
	normalized := normalizePath(path)
	if core.PathIsAbs(normalized) {
		return normalized
	}
	cwd := currentDir()
	if cwd == "" || cwd == "." {
		return normalized
	}
	return normalizePath(core.JoinPath(cwd, normalized))
}

func currentDir() string {
	if cwd := core.Getwd(); cwd.OK && cwd.Value.(string) != "" {
		return normalizePath(cwd.Value.(string))
	}
	cwd := normalizePath(core.Env("PWD"))
	if cwd != "" && cwd != "." {
		return cwd
	}
	return normalizePath(core.Env("DIR_CWD"))
}

func (c *Cache) ensureConfigured(op string) core.Result {
	if c == nil {
		return failure(op, "cache is nil", nil)
	}
	if c.baseDir == "" {
		return failure(op, "cache base directory is empty; construct with cache.New", nil)
	}
	if c.runtime == nil {
		return failure(op, "cache runtime is nil; construct with cache.New", nil)
	}
	return core.Ok(nil)
}

func (c *Cache) ensureReady(op string) core.Result {
	if r := c.ensureConfigured(op); !r.OK {
		return r
	}
	if c.medium == nil {
		return failure(op, "cache medium is nil; construct with cache.New", nil)
	}
	return core.Ok(nil)
}

func failure(op, message string, cause error) core.Result {
	return core.Fail(core.E(op, message, cause))
}

func resultCause(result core.Result) core.Result {
	if err, ok := result.Value.(error); ok {
		return core.Ok(err)
	}
	return core.Ok(core.E("cache.result", "unexpected result failure", nil))
}
