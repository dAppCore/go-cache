// SPDX-License-Identifier: EUPL-1.2

// Package cache provides a storage-agnostic, JSON-based cache backed by any io.Medium.
package cache

import (
	// Note: AX-6 — structural: coreio.Medium surfaces fs.ErrNotExist/fs.DirEntry, and Lstat symlink checks use fs.ModeSymlink.
	"io/fs"
	// Note: AX-6 — intrinsic: coreio.Medium has no no-follow Lstat primitive or dynamic cwd lookup.
	"os"
	"slices"
	// Note: AX-6 — no core equivalent for durations or wall-clock timestamps.
	"time"

	"dappco.re/go/core"
	coreio "dappco.re/go/io"
)

// DefaultTTL is the default cache expiry time.
//
// Usage example:
//
//	c, err := cache.New(coreio.NewMockMedium(), "/tmp/cache", cache.DefaultTTL)
const DefaultTTL = 1 * time.Hour

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

// Cache stores JSON-encoded entries in a Medium-backed cache rooted at baseDir.
//
//	c, err := cache.New(coreio.Local, "/tmp/cache", 5*time.Minute)
type Cache struct {
	medium       coreio.Medium
	baseDir      string
	cacheTTL     time.Duration
	invalidation map[string][]InvalidateFunc
	runtime      *core.Core
	// Backing-store operations intentionally have no mutex; callers must
	// synchronize concurrent writes to the same key.
}

// Entry is the serialized cache record written to the backing Medium.
//
//	entry := cache.Entry{
//		Data:      []byte(`{"foo":"bar"}`),
//		CachedAt:  time.Now(),
//		ExpiresAt: time.Now().Add(time.Hour),
//	}
type Entry struct {
	Data      rawJSON   `json:"data"`
	CachedAt  time.Time `json:"cached_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type rawJSON []byte

func (raw rawJSON) MarshalJSON() ([]byte, error) {
	if raw == nil {
		return []byte("null"), nil
	}
	return raw, nil
}

func (raw *rawJSON) UnmarshalJSON(data []byte) error {
	if raw == nil {
		return core.E("cache.rawJSON.UnmarshalJSON", "target is nil", nil)
	}
	*raw = append((*raw)[0:0], data...)
	return nil
}

func marshalPrettyJSON(value any) (string, error) {
	result := core.JSONMarshal(value)
	if !result.OK {
		return "", result.Value.(error)
	}
	return indentJSON([]byte(core.JSONMarshalString(value))), nil
}

func indentJSON(data []byte) string {
	builder := core.NewBuilder()
	indent := 0
	inString := false
	escaped := false

	writeIndent := func() {
		for i := 0; i < indent; i++ {
			builder.WriteString("  ")
		}
	}

	for i, c := range data {
		if inString {
			builder.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
			builder.WriteByte(c)
		case '{', '[':
			builder.WriteByte(c)
			next := nextNonJSONSpace(data, i+1)
			if next >= 0 && ((c == '{' && data[next] == '}') || (c == '[' && data[next] == ']')) {
				continue
			}
			indent++
			builder.WriteByte('\n')
			writeIndent()
		case '}', ']':
			previous := previousNonJSONSpace(data, i-1)
			if previous >= 0 && ((c == '}' && data[previous] == '{') || (c == ']' && data[previous] == '[')) {
				builder.WriteByte(c)
				continue
			}
			if indent > 0 {
				indent--
			}
			builder.WriteByte('\n')
			writeIndent()
			builder.WriteByte(c)
		case ',':
			builder.WriteByte(c)
			builder.WriteByte('\n')
			writeIndent()
		case ':':
			builder.WriteString(": ")
		default:
			if !isJSONSpace(c) {
				builder.WriteByte(c)
			}
		}
	}

	return builder.String()
}

func nextNonJSONSpace(data []byte, start int) int {
	for i := start; i < len(data); i++ {
		if !isJSONSpace(data[i]) {
			return i
		}
	}
	return -1
}

func previousNonJSONSpace(data []byte, start int) int {
	for i := start; i >= 0; i-- {
		if !isJSONSpace(data[i]) {
			return i
		}
	}
	return -1
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

// BinaryMeta is the metadata for binary cache payloads.
//
//	{
//	  "content_type":"application/wasm",
//	  "size":1048576,
//	  "cached_at":"2026-04-14T00:00:00Z",
//	  "expires_at":"2026-04-15T00:00:00Z"
//	}
type BinaryMeta struct {
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CachedAt    time.Time `json:"cached_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// InvalidateFunc returns glob patterns to delete when a registered trigger fires.
//
//	c.OnInvalidate("dns.tree-root-changed", func(trigger string) []string {
//		return []string{"dns/*"}
//	})
type InvalidateFunc func(trigger string) []string

// New creates a cache with explicit storage, root directory, and TTL.
//
//	c, err := cache.New(coreio.Local, "/tmp/cache", 5*time.Minute)
//	c, err = cache.New(nil, "", 0) // uses Local, .core/cache, and DefaultTTL
func New(medium coreio.Medium, baseDir string, cacheTTL time.Duration) (*Cache, error) {
	if medium == nil {
		medium = coreio.Local
	}

	if baseDir == "" {
		cwd := currentDir()
		if cwd == "" || cwd == "." {
			return nil, core.E("cache.New", "failed to resolve current working directory", nil)
		}

		baseDir = normalizePath(core.JoinPath(cwd, ".core", "cache"))
	} else {
		baseDir = absolutePath(baseDir)
	}

	if cacheTTL < 0 {
		return nil, core.E("cache.New", "ttl must be >= 0", nil)
	}

	if cacheTTL == 0 {
		cacheTTL = DefaultTTL
	}

	if err := medium.EnsureDir(baseDir); err != nil {
		return nil, core.E("cache.New", "failed to create cache directory", err)
	}

	return &Cache{
		medium:       medium,
		baseDir:      baseDir,
		cacheTTL:     cacheTTL,
		invalidation: make(map[string][]InvalidateFunc),
		runtime:      core.New(),
	}, nil
}

// Path resolves the on-disk JSON path for a cache key.
//
//	path, err := c.Path("github/acme/repos")
//	// => /tmp/cache/github/acme/repos.json
func (cache *Cache) Path(key string) (string, error) {
	if err := cache.ensureConfigured("cache.Path"); err != nil {
		return "", err
	}

	if err := ensureSafeKey(key); err != nil {
		return "", err
	}

	baseDir := absolutePath(cache.baseDir)
	path := absolutePath(core.JoinPath(baseDir, key+".json"))
	pathPrefix := normalizePath(core.Concat(baseDir, pathSeparator()))

	if path != baseDir && !core.HasPrefix(path, pathPrefix) {
		return "", core.E("cache.Path", "invalid cache key: path traversal attempt", nil)
	}
	if err := ensureNoSymlinkPath(baseDir, path); err != nil {
		return "", core.E("cache.Path", "invalid cache key: symlink escape attempt", err)
	}

	return path, nil
}

// entryPaths resolves the JSON and binary file paths for a cache key.
//
//	jsonPath, binPath, err := c.entryPaths("github/acme/repos")
func (cache *Cache) entryPaths(key string) (string, string, error) {
	jsonPath, err := cache.Path(key)
	if err != nil {
		return "", "", err
	}

	baseDir := absolutePath(cache.baseDir)
	binaryPath := absolutePath(core.JoinPath(baseDir, key+".bin"))
	return jsonPath, binaryPath, nil
}

// Get unmarshals the cached item into dest if it exists and has not expired.
//
//	found, err := c.Get("github/acme/repos", &repos)
func (cache *Cache) Get(key string, dest any) (bool, error) {
	if err := cache.ensureReady("cache.Get"); err != nil {
		return false, err
	}

	path, err := cache.Path(key)
	if err != nil {
		return false, err
	}

	dataStr, err := cache.medium.Read(path)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, core.E("cache.Get", "failed to read cache file", err)
	}

	var entry Entry
	entryResult := core.JSONUnmarshalString(dataStr, &entry)
	if !entryResult.OK {
		return false, core.E("cache.Get", "failed to unmarshal cache entry", entryResult.Value.(error))
	}

	if time.Now().After(entry.ExpiresAt) {
		return false, nil
	}

	if err := core.JSONUnmarshal(entry.Data, dest); !err.OK {
		return false, core.E("cache.Get", "failed to unmarshal cached data", err.Value.(error))
	}

	return true, nil
}

// Set stores a value using the cache's default TTL.
//
//	err := c.Set("github/acme/repos", repos)
//	err = c.Set("config/theme", "dark")
func (cache *Cache) Set(key string, data any) error {
	if err := cache.ensureReady("cache.Set"); err != nil {
		return err
	}
	return cache.set(key, data, cache.defaultTTL(), true)
}

// SetWithTTL stores a value with an explicit TTL override.
//
//	err := c.SetWithTTL("dns/example.com/A", records, 5*time.Minute)
//	err = c.SetWithTTL("session/token", token, 30*time.Second)
func (cache *Cache) SetWithTTL(key string, data any, ttl time.Duration) error {
	if err := cache.ensureReady("cache.SetWithTTL"); err != nil {
		return err
	}
	return cache.set(key, data, ttl, false)
}

func (cache *Cache) set(key string, data any, ttl time.Duration, useDefaultTTL bool) error {
	if err := cache.ensureReady("cache.set"); err != nil {
		return err
	}

	path, _, err := cache.entryPaths(key)
	if err != nil {
		return err
	}

	snapshot, err := readFileSnapshot(cache.medium, path)
	if err != nil {
		return core.E("cache.set", "failed to inspect existing cache entry", err)
	}

	if err := cache.medium.EnsureDir(core.PathDir(path)); err != nil {
		return core.E("cache.Set", "failed to create directory", err)
	}

	dataResult := core.JSONMarshal(data)
	if !dataResult.OK {
		return core.E("cache.Set", "failed to marshal cache data", dataResult.Value.(error))
	}

	if ttl < 0 {
		return core.E("cache.set", "cache ttl must be >= 0", nil)
	}
	if ttl == 0 && useDefaultTTL {
		ttl = cache.defaultTTL()
	}

	now := time.Now()
	entry := Entry{
		Data:      rawJSON(dataResult.Value.([]byte)),
		CachedAt:  now,
		ExpiresAt: now.Add(ttl),
	}

	entryJSON, err := marshalPrettyJSON(entry)
	if err != nil {
		return core.E("cache.Set", "failed to marshal cache entry", err)
	}

	if err := cache.medium.Write(path, entryJSON); err != nil {
		_ = restoreFileSnapshot(cache.medium, snapshot)
		return core.E("cache.set", "failed to write cache file", err)
	}
	return nil
}

// Delete removes one cached entry.
//
//	err := c.Delete("github/acme/repos")
func (cache *Cache) Delete(key string) error {
	if err := cache.ensureReady("cache.Delete"); err != nil {
		return err
	}

	_, err := cache.removeEntryFiles(key)
	if core.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// removeEntryFiles deletes both the JSON metadata and sidecar binary payload for a key.
func (cache *Cache) removeEntryFiles(key string) (bool, error) {
	if err := cache.ensureReady("cache.removeEntryFiles"); err != nil {
		return false, err
	}

	jsonPath, binaryPath, err := cache.entryPaths(key)
	if err != nil {
		return false, err
	}

	removed := false
	if err := cache.medium.Delete(jsonPath); err != nil {
		if !core.Is(err, fs.ErrNotExist) {
			return removed, core.E("cache.removeEntryFiles", "failed to delete cache json file", err)
		}
	} else {
		removed = true
	}

	if err := cache.medium.Delete(binaryPath); err != nil {
		if !core.Is(err, fs.ErrNotExist) {
			return removed, core.E("cache.removeEntryFiles", "failed to delete cache binary file", err)
		}
	} else {
		removed = true
	}

	return removed, nil
}

// SetBinary stores raw bytes in a sidecar `.bin` file and metadata in JSON.
//
//	err := c.SetBinary("wasm/my-module", wasmBytes, "application/wasm")
//	err = c.SetBinary("artifacts/logo", pngBytes, "image/png")
func (cache *Cache) SetBinary(key string, data []byte, contentType string) error {
	if err := cache.ensureReady("cache.SetBinary"); err != nil {
		return err
	}
	return cache.setBinary(key, data, contentType, cache.defaultTTL(), true)
}

// SetBinaryWithTTL stores raw bytes with an explicit TTL override.
//
//	err := c.SetBinaryWithTTL("responses/temp", body, "text/html", 10*time.Minute)
//	err = c.SetBinaryWithTTL("dns/example.com/AAAA", raw, "application/octet-stream", 15*time.Second)
func (cache *Cache) SetBinaryWithTTL(key string, data []byte, contentType string, ttl time.Duration) error {
	if err := cache.ensureReady("cache.SetBinaryWithTTL"); err != nil {
		return err
	}
	return cache.setBinary(key, data, contentType, ttl, false)
}

func (cache *Cache) setBinary(key string, data []byte, contentType string, ttl time.Duration, useDefaultTTL bool) error {
	if err := cache.ensureReady("cache.setBinary"); err != nil {
		return err
	}

	jsonPath, binaryPath, err := cache.entryPaths(key)
	if err != nil {
		return err
	}

	jsonSnapshot, err := readFileSnapshot(cache.medium, jsonPath)
	if err != nil {
		return core.E("cache.setBinary", "failed to inspect existing binary metadata", err)
	}
	binarySnapshot, err := readFileSnapshot(cache.medium, binaryPath)
	if err != nil {
		return core.E("cache.setBinary", "failed to inspect existing binary payload", err)
	}

	if ttl < 0 {
		return core.E("cache.setBinary", "cache ttl must be >= 0", nil)
	}
	if ttl == 0 && useDefaultTTL {
		ttl = cache.defaultTTL()
	}

	if err := cache.medium.EnsureDir(core.PathDir(jsonPath)); err != nil {
		return core.E("cache.setBinary", "failed to create directory", err)
	}

	now := time.Now()
	meta := BinaryMeta{
		ContentType: contentType,
		Size:        int64(len(data)),
		CachedAt:    now,
		ExpiresAt:   now.Add(ttl),
	}

	metaJSON, err := marshalPrettyJSON(meta)
	if err != nil {
		return core.E("cache.setBinary", "failed to marshal binary metadata", err)
	}

	if err := cache.medium.Write(binaryPath, string(data)); err != nil {
		_ = restoreFileSnapshot(cache.medium, jsonSnapshot)
		_ = restoreFileSnapshot(cache.medium, binarySnapshot)
		return core.E("cache.setBinary", "failed to write binary payload", err)
	}

	if err := cache.medium.Write(jsonPath, metaJSON); err != nil {
		_ = restoreFileSnapshot(cache.medium, binarySnapshot)
		_ = restoreFileSnapshot(cache.medium, jsonSnapshot)
		return core.E("cache.setBinary", "failed to write binary metadata", err)
	}

	return nil
}

// GetBinary returns raw binary cache payload.
//
//	data, found, err := c.GetBinary("wasm/my-module")
func (cache *Cache) GetBinary(key string) ([]byte, bool, error) {
	if err := cache.ensureReady("cache.GetBinary"); err != nil {
		return nil, false, err
	}

	metaPath, binaryPath, err := cache.entryPaths(key)
	if err != nil {
		return nil, false, err
	}

	rawMeta, err := cache.medium.Read(metaPath)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, core.E("cache.GetBinary", "failed to read binary metadata", err)
	}

	var meta BinaryMeta
	metaResult := core.JSONUnmarshalString(rawMeta, &meta)
	if !metaResult.OK {
		return nil, false, core.E("cache.GetBinary", "failed to unmarshal binary metadata", metaResult.Value.(error))
	}

	if time.Now().After(meta.ExpiresAt) {
		return nil, false, nil
	}

	body, err := cache.medium.Read(binaryPath)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, core.E("cache.GetBinary", "failed to read binary data", err)
	}

	return []byte(body), true, nil
}

// DeleteMany removes several entries in one call. Missing keys are ignored.
//
//	err := c.DeleteMany("github/acme/repos", "github/acme/meta")
//	err = c.DeleteMany("dns/example.com/A", "dns/example.com/AAAA")
func (cache *Cache) DeleteMany(keys ...string) error {
	if err := cache.ensureReady("cache.DeleteMany"); err != nil {
		return err
	}

	type entryFileSet struct {
		jsonPath   string
		binaryPath string
	}

	resolved := make([]entryFileSet, 0, len(keys))
	for _, key := range keys {
		jsonPath, binaryPath, err := cache.entryPaths(key)
		if err != nil {
			return err
		}
		resolved = append(resolved, entryFileSet{jsonPath: jsonPath, binaryPath: binaryPath})
	}

	for _, paths := range resolved {
		if err := cache.medium.Delete(paths.jsonPath); err != nil && !core.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := cache.medium.Delete(paths.binaryPath); err != nil && !core.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	return nil
}

func (cache *Cache) listJSONKeys() ([]string, error) {
	keys, err := cache.collectJSONKeys("")
	if err != nil {
		return nil, err
	}
	slices.Sort(keys)
	return keys, nil
}

func (cache *Cache) collectJSONKeys(prefix string) ([]string, error) {
	listPath := cache.baseDir
	if prefix != "" {
		listPath = core.JoinPath(cache.baseDir, prefix)
	}

	entries, err := cache.medium.List(listPath)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, core.E("cache.collectJSONKeys", "failed to list cache directory", err)
	}

	var keys []string
	for _, entry := range entries {
		name := entry.Name()
		childPrefix := name
		if prefix != "" {
			childPrefix = core.JoinPath(prefix, name)
		}

		if entry.IsDir() {
			childKeys, err := cache.collectJSONKeys(childPrefix)
			if err != nil {
				return nil, err
			}
			keys = append(keys, childKeys...)
			continue
		}

		if core.HasSuffix(name, ".json") {
			keys = append(keys, core.TrimSuffix(childPrefix, ".json"))
		}
	}
	return keys, nil
}

func (cache *Cache) keysByPattern(pattern string) ([]string, error) {
	if err := ensureSafePattern(pattern); err != nil {
		return nil, err
	}

	allKeys, err := cache.listJSONKeys()
	if err != nil {
		return nil, err
	}

	var matched []string
	for _, key := range allKeys {
		ok, err := matchKeyPattern(pattern, key)
		if err != nil {
			return nil, core.E("cache.keysByPattern", "failed to match pattern", err)
		}
		if ok {
			matched = append(matched, key)
		}
	}
	return matched, nil
}

func (cache *Cache) clearScope(prefix string) error {
	keys, err := cache.keysByPattern(prefix)
	if err != nil {
		return err
	}
	descendants, err := cache.keysByPattern(prefix + "/*")
	if err != nil {
		return err
	}
	keys = append(keys, descendants...)

	for _, key := range keys {
		if _, err := cache.removeEntryFiles(key); err != nil {
			return err
		}
	}

	return nil
}

// matchKeyPattern reports whether key matches the glob pattern.
//
// Supported patterns per RFC §12.4:
//
//	"dns/*"           — all keys under dns/ (any depth)
//	"dns/charon.*"    — dns/charon.lthn, dns/charon.local, etc. (single segment)
//	"scope_a1b2c3/*"  — all keys in a specific scope (any depth)
//	"exact-key"       — single key (no wildcard)
func matchKeyPattern(pattern, key string) (bool, error) {
	if !containsAnyGlob(pattern) {
		return pattern == key, nil
	}

	// A trailing "/*" means "all descendants of this prefix" — any depth.
	if core.HasSuffix(pattern, "/*") {
		prefix := core.TrimSuffix(pattern, "/*")
		if prefix == "" {
			return true, nil
		}
		return core.HasPrefix(key, prefix+"/"), nil
	}

	// Otherwise match a single path segment against the last pattern segment.
	patternParts := core.Split(pattern, "/")
	keyParts := core.Split(key, "/")
	if len(patternParts) != len(keyParts) {
		return false, nil
	}
	for i, part := range patternParts {
		if !containsAnyGlob(part) {
			if part != keyParts[i] {
				return false, nil
			}
			continue
		}
		ok, err := segmentMatch(part, keyParts[i])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// containsAnyGlob reports whether s contains any glob metacharacter.
//
//	containsAnyGlob("dns/*") // true
//	containsAnyGlob("exact") // false
func containsAnyGlob(s string) bool {
	for _, r := range s {
		if r == '*' || r == '?' || r == '[' || r == ']' {
			return true
		}
	}
	return false
}

// segmentMatch matches pattern against name within a single path segment.
// Supports '*' (any run of non-separator chars) and literal characters.
//
//	segmentMatch("charon.*", "charon.lthn") // true
//	segmentMatch("charon.*", "other.lthn")  // false
func segmentMatch(pattern, name string) (bool, error) {
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
		return false, nil
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern), nil
}

// OnInvalidate registers a trigger callback that returns patterns to delete.
//
//	c.OnInvalidate("dns.tree-root-changed", func(trigger string) []string {
//		return []string{"dns/*"}
//	})
func (cache *Cache) OnInvalidate(trigger string, fn InvalidateFunc) {
	if err := cache.ensureReady("cache.OnInvalidate"); err != nil {
		return
	}
	if fn == nil {
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
//
//	deleted, err := c.Invalidate("dns.tree-root-changed")
func (cache *Cache) Invalidate(trigger string) (int, error) {
	if err := cache.ensureReady("cache.Invalidate"); err != nil {
		return 0, err
	}

	lock := cache.runtime.Lock("cache")
	lock.Mutex.RLock()
	callbacks := append([]InvalidateFunc(nil), cache.invalidation[trigger]...)
	lock.Mutex.RUnlock()
	total := 0
	for _, callback := range callbacks {
		for _, pattern := range callback(trigger) {
			if pattern == "" {
				continue
			}
			matches, err := cache.keysByPattern(pattern)
			if err != nil {
				return total, err
			}
			for _, key := range matches {
				removed, err := cache.removeEntryFiles(key)
				if err != nil {
					return total, err
				}
				if removed {
					total++
				}
			}
		}
	}

	return total, nil
}

// Scoped returns a cache namespaced by origin hash.
//
//	scoped := c.Scoped("https://app.example.com")
//	_ = scoped.Set("user/profile", profile)
func (cache *Cache) Scoped(origin string) *ScopedCache {
	if cache == nil {
		return nil
	}
	return &ScopedCache{
		parent: cache,
		prefix: scopePrefix(origin),
	}
}

// ClearScope removes cache entries for a scoped origin.
//
//	err := c.ClearScope("https://app.example.com")
func (cache *Cache) ClearScope(origin string) error {
	if err := cache.ensureReady("cache.ClearScope"); err != nil {
		return err
	}

	prefix := scopePrefix(origin)
	if err := ensureSafeKey(prefix); err != nil {
		return err
	}
	return cache.clearScope(prefix)
}

func (cache *Cache) defaultTTL() time.Duration {
	if cache.cacheTTL <= 0 {
		return DefaultTTL
	}
	return cache.cacheTTL
}

func ensureSafeKey(key string) error {
	if key == "" {
		return core.E("cache.validateKey", "invalid empty key", nil)
	}
	if len(key) > maxCacheKeyBytes {
		return core.E("cache.validateKey", "invalid key: too long", nil)
	}
	if core.Contains(key, "\\") {
		return core.E("cache.validateKey", "invalid key: contains path separators", nil)
	}
	if hasPathDangerousBytes(key) {
		return core.E("cache.validateKey", "invalid key: contains control bytes", nil)
	}

	for _, part := range core.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return core.E("cache.validateKey", "invalid key: path traversal attempt", nil)
		}
	}

	return nil
}

func ensureSafePattern(pattern string) error {
	if pattern == "" {
		return core.E("cache.validatePattern", "invalid empty pattern", nil)
	}
	if len(pattern) > maxCachePatternBytes {
		return core.E("cache.validatePattern", "invalid pattern: too long", nil)
	}
	if core.Contains(pattern, "\\") || hasPathDangerousBytes(pattern) {
		return core.E("cache.validatePattern", "invalid pattern: contains control bytes", nil)
	}
	return nil
}

func ensureNoSymlinkPath(baseDir, path string) error {
	if err := rejectSymlink(baseDir); err != nil {
		return err
	}

	if path == baseDir {
		return nil
	}

	rel := core.TrimPrefix(path, normalizePath(core.Concat(baseDir, pathSeparator())))
	if rel == path {
		return nil
	}

	current := baseDir
	for _, part := range core.Split(rel, pathSeparator()) {
		if part == "" {
			continue
		}
		current = core.JoinPath(current, part)
		if err := rejectSymlink(current); err != nil {
			return err
		}
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return core.E("cache.validatePath", "path contains symlink", nil)
	}
	return nil
}

func hasPathDangerousBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func ensureSafeResponseBodyPath(path string) error {
	if path == "" {
		return core.E("cache.validateResponseBodyPath", "invalid empty body path", nil)
	}
	if len(path) > maxCacheKeyBytes {
		return core.E("cache.validateResponseBodyPath", "invalid body path: too long", nil)
	}
	if core.PathIsAbs(path) {
		return core.E("cache.validateResponseBodyPath", "invalid body path: absolute paths are not allowed", nil)
	}
	if core.Contains(path, "\\") || hasPathDangerousBytes(path) {
		return core.E("cache.validateResponseBodyPath", "invalid body path: contains control bytes", nil)
	}

	normalized := normalizePath(path)
	if !core.HasPrefix(normalized, "responses/") || !core.HasSuffix(normalized, ".bin") {
		return core.E("cache.validateResponseBodyPath", "invalid body path: expected responses/<key>.bin", nil)
	}

	rel := core.TrimPrefix(normalized, "responses/")
	rel = core.TrimSuffix(rel, ".bin")
	if rel == "" {
		return core.E("cache.validateResponseBodyPath", "invalid body path", nil)
	}

	for _, segment := range core.Split(rel, "/") {
		if err := ensureSafeKey(segment); err != nil {
			return core.E("cache.validateResponseBodyPath", "invalid body path", err)
		}
	}

	return nil
}

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
//
//	admin := scoped.Scoped("https://admin.example.com")
//	_ = admin.Set("user/profile", profile)
func (scopedCache *ScopedCache) Scoped(origin string) *ScopedCache {
	if scopedCache == nil || scopedCache.parent == nil {
		return nil
	}
	return scopedCache.parent.Scoped(origin)
}

func (scopedCache *ScopedCache) Path(key string) (string, error) {
	if scopedCache == nil || scopedCache.parent == nil {
		return "", core.E("cache.Scoped.Path", "scoped cache is nil", nil)
	}
	return scopedCache.parent.Path(scopedCache.fullKey(key))
}

func (scopedCache *ScopedCache) Get(key string, dest any) (bool, error) {
	if scopedCache == nil || scopedCache.parent == nil {
		return false, core.E("cache.Scoped.Get", "scoped cache is nil", nil)
	}
	return scopedCache.parent.Get(scopedCache.fullKey(key), dest)
}

func (scopedCache *ScopedCache) Set(key string, value any) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.Set", "scoped cache is nil", nil)
	}
	return scopedCache.parent.Set(scopedCache.fullKey(key), value)
}

func (scopedCache *ScopedCache) SetWithTTL(key string, value any, ttl time.Duration) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.SetWithTTL", "scoped cache is nil", nil)
	}
	return scopedCache.parent.SetWithTTL(scopedCache.fullKey(key), value, ttl)
}

func (scopedCache *ScopedCache) SetBinary(key string, data []byte, contentType string) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.SetBinary", "scoped cache is nil", nil)
	}
	return scopedCache.parent.SetBinary(scopedCache.fullKey(key), data, contentType)
}

func (scopedCache *ScopedCache) SetBinaryWithTTL(key string, data []byte, contentType string, ttl time.Duration) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.SetBinaryWithTTL", "scoped cache is nil", nil)
	}
	return scopedCache.parent.SetBinaryWithTTL(scopedCache.fullKey(key), data, contentType, ttl)
}

func (scopedCache *ScopedCache) GetBinary(key string) ([]byte, bool, error) {
	if scopedCache == nil || scopedCache.parent == nil {
		return nil, false, core.E("cache.Scoped.GetBinary", "scoped cache is nil", nil)
	}
	return scopedCache.parent.GetBinary(scopedCache.fullKey(key))
}

func (scopedCache *ScopedCache) Delete(key string) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.Delete", "scoped cache is nil", nil)
	}
	return scopedCache.parent.Delete(scopedCache.fullKey(key))
}

func (scopedCache *ScopedCache) DeleteMany(keys ...string) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.DeleteMany", "scoped cache is nil", nil)
	}
	full := make([]string, len(keys))
	for i, key := range keys {
		full[i] = scopedCache.fullKey(key)
	}
	return scopedCache.parent.DeleteMany(full...)
}

// Clear removes all entries in the scope.
//
//	err := scoped.Clear()
func (scopedCache *ScopedCache) Clear() error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.Clear", "scoped cache is nil", nil)
	}
	return scopedCache.parent.clearScope(scopedCache.prefix)
}

// ClearScope removes cache entries for a scoped origin.
//
//	err := scoped.ClearScope("https://app.example.com")
func (scopedCache *ScopedCache) ClearScope(origin string) error {
	if scopedCache == nil || scopedCache.parent == nil {
		return core.E("cache.Scoped.ClearScope", "scoped cache is nil", nil)
	}
	return scopedCache.parent.ClearScope(origin)
}

func (scopedCache *ScopedCache) OnInvalidate(trigger string, fn InvalidateFunc) {
	if scopedCache == nil || scopedCache.parent == nil {
		return
	}
	if fn == nil {
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
			if pattern == "" {
				continue
			}
			scopedPatterns = append(scopedPatterns, scopePattern(prefix, pattern))
		}
		return scopedPatterns
	})
}

func (scopedCache *ScopedCache) Invalidate(trigger string) (int, error) {
	if scopedCache == nil || scopedCache.parent == nil {
		return 0, core.E("cache.Scoped.Invalidate", "scoped cache is nil", nil)
	}
	return scopedCache.parent.Invalidate(trigger)
}

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
//
//	storage, _ := cache.NewCacheStorage(coreio.Local, "/tmp/cache-storage")
//	appCache, err := storage.Open("my-app-v1")
//	defer storage.Close()
type CacheStorage struct {
	medium  coreio.Medium
	baseDir string
	caches  map[string]*HTTPCache
	runtime *core.Core
}

// NewCacheStorage creates a namespace container for HTTPCache instances.
//
//	storage, err := cache.NewCacheStorage(coreio.Local, "/tmp/cache-storage")
func NewCacheStorage(medium coreio.Medium, baseDir string) (*CacheStorage, error) {
	if medium == nil {
		medium = coreio.Local
	}

	if baseDir == "" {
		cwd := currentDir()
		if cwd == "" || cwd == "." {
			return nil, core.E("cache.NewCacheStorage", "failed to resolve current working directory", nil)
		}
		baseDir = normalizePath(core.JoinPath(cwd, ".core", "cache-storage"))
	} else {
		baseDir = absolutePath(baseDir)
	}

	if err := medium.EnsureDir(baseDir); err != nil {
		return nil, core.E("cache.NewCacheStorage", "failed to create cache storage directory", err)
	}

	return &CacheStorage{
		medium:  medium,
		baseDir: baseDir,
		caches:  make(map[string]*HTTPCache),
		runtime: core.New(),
	}, nil
}

// Open retrieves a named HTTPCache, creating it on first use.
//
//	staticCache, err := storage.Open("static-assets-v2")
//	api, err := storage.Open("api-responses")
func (storage *CacheStorage) Open(name string) (*HTTPCache, error) {
	if err := storage.ensureReady("cache.CacheStorage.Open"); err != nil {
		return nil, err
	}
	if err := ensureSafeCacheName("cache.CacheStorage.Open", name); err != nil {
		return nil, err
	}

	lock := storage.runtime.Lock("cache-storage")
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if httpCache, ok := storage.caches[name]; ok {
		return httpCache, nil
	}

	cacheDir := core.JoinPath(storage.baseDir, name)
	if err := storage.medium.EnsureDir(cacheDir); err != nil {
		return nil, core.E("cache.CacheStorage.Open", "failed to create cache directory", err)
	}

	httpCache := &HTTPCache{
		name:    name,
		medium:  storage.medium,
		baseDir: cacheDir,
	}
	storage.caches[name] = httpCache
	return httpCache, nil
}

// Delete removes a named HTTP cache and all entries.
//
//	err := storage.Delete("static-assets-v1")
//	err = storage.Delete("old-cache")
func (storage *CacheStorage) Delete(name string) error {
	if err := storage.ensureReady("cache.CacheStorage.Delete"); err != nil {
		return err
	}
	if err := ensureSafeCacheName("cache.CacheStorage.Delete", name); err != nil {
		return err
	}

	lock := storage.runtime.Lock("cache-storage")
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if err := storage.medium.DeleteAll(core.JoinPath(storage.baseDir, name)); err != nil && !core.Is(err, fs.ErrNotExist) {
		return core.E("cache.CacheStorage.Delete", "failed to delete cache directory", err)
	}

	delete(storage.caches, name)
	return nil
}

// ensureSafeCacheName rejects empty, path-separator, or traversal cache names.
func ensureSafeCacheName(op, name string) error {
	if name == "" {
		return core.E(op, "cache name is empty", nil)
	}
	if len(name) > maxCacheNameBytes {
		return core.E(op, "invalid cache name: too long", nil)
	}
	if core.Contains(name, "/") || core.Contains(name, `\`) {
		return core.E(op, "invalid cache name", nil)
	}
	if hasPathDangerousBytes(name) {
		return core.E(op, "invalid cache name", nil)
	}
	if name == "." || name == ".." {
		return core.E(op, "invalid cache name", nil)
	}
	return nil
}

// Keys lists all named caches.
//
//	names, err := storage.Keys()
//	// ["static-assets-v2", "api-responses"]
func (storage *CacheStorage) Keys() ([]string, error) {
	if err := storage.ensureReady("cache.CacheStorage.Keys"); err != nil {
		return nil, err
	}

	lock := storage.runtime.Lock("cache-storage")
	lock.Mutex.RLock()
	names := make(map[string]struct{}, len(storage.caches))
	for name := range storage.caches {
		names[name] = struct{}{}
	}
	lock.Mutex.RUnlock()

	entries, err := storage.medium.List(storage.baseDir)
	if err != nil {
		if !core.Is(err, fs.ErrNotExist) {
			return nil, core.E("cache.CacheStorage.Keys", "failed to list caches", err)
		}
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
	return out, nil
}

// Close releases storage resources for compatibility with long-lived workflows.
//
//	_ = storage.Close()
//	appCache, err := storage.Open("reused-cache")
func (storage *CacheStorage) Close() error {
	if storage == nil {
		return nil
	}
	if storage.runtime == nil {
		storage.caches = make(map[string]*HTTPCache)
		return nil
	}
	lock := storage.runtime.Lock("cache-storage")
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	storage.caches = make(map[string]*HTTPCache)
	return nil
}

// HTTPCache stores request/response pairs.
//
//	storage, _ := cache.NewCacheStorage(coreio.Local, "/tmp/cache-storage")
//	appCache, _ := storage.Open("my-app-v1")
//	err := appCache.Put(req, resp, body)
type HTTPCache struct {
	name    string
	medium  coreio.Medium
	baseDir string
}

func (storage *CacheStorage) ensureReady(op string) error {
	if storage == nil {
		return core.E(op, "cache storage is nil", nil)
	}
	if storage.medium == nil {
		return core.E(op, "cache storage medium is nil; construct via cache.NewCacheStorage", nil)
	}
	if storage.baseDir == "" {
		return core.E(op, "cache storage base directory is empty; construct via cache.NewCacheStorage", nil)
	}
	if storage.runtime == nil {
		return core.E(op, "cache storage runtime is nil; construct via cache.NewCacheStorage", nil)
	}
	lock := storage.runtime.Lock("cache-storage")
	lock.Mutex.Lock()
	defer lock.Mutex.Unlock()
	if storage.caches == nil {
		storage.caches = make(map[string]*HTTPCache)
	}
	return nil
}

func (httpCache *HTTPCache) ensureReady(op string) error {
	if httpCache == nil {
		return core.E(op, "http cache is nil", nil)
	}
	if httpCache.medium == nil {
		return core.E(op, "http cache medium is nil; construct via cache.CacheStorage.Open", nil)
	}
	if httpCache.baseDir == "" {
		return core.E(op, "http cache base directory is empty; construct via cache.CacheStorage.Open", nil)
	}
	return nil
}

// CachedRequest identifies a request by URL and method.
//
//	req := cache.CachedRequest{
//		URL:    "https://api.example.com/users",
//		Method: "GET",
//	}
type CachedRequest struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

// CachedResponse stores HTTP metadata for a cached response body.
//
//	resp := cache.CachedResponse{
//		Status:     200,
//		StatusText: "OK",
//		Headers:    map[string]string{"Content-Type": "application/json"},
//		BodyPath:   "responses/a1b2c3.bin",
//	}
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

func (httpCache *HTTPCache) requestKey(req CachedRequest) (string, error) {
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

func rawBase64URLDecode(encoded string) ([]byte, error) {
	if core.Contains(encoded, "=") {
		return nil, core.E("cache.rawBase64URLDecode", "raw URL base64 must not contain padding", nil)
	}
	if len(encoded)%4 == 1 {
		return nil, core.E("cache.rawBase64URLDecode", "invalid raw URL base64 length", nil)
	}

	out := make([]byte, 0, len(encoded)*3/4)
	for i := 0; i < len(encoded); {
		remaining := len(encoded) - i
		chunkLen := 4
		if remaining < chunkLen {
			chunkLen = remaining
		}

		var values [4]byte
		for j := 0; j < chunkLen; j++ {
			value := rawBase64URLDecodeValue(encoded[i+j])
			if value < 0 {
				return nil, core.E("cache.rawBase64URLDecode", "invalid raw URL base64 character", nil)
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

	return out, nil
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

func decodeRequestKey(encoded string) (CachedRequest, error) {
	raw, err := rawBase64URLDecode(encoded)
	if err != nil {
		return CachedRequest{}, core.E("cache.decodeRequestKey", "invalid cached request key", err)
	}
	parts := core.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return CachedRequest{}, core.E("cache.decodeRequestKey", "invalid cached request key payload", nil)
	}

	return CachedRequest{
		Method: parts[0],
		URL:    parts[1],
	}, nil
}

func (httpCache *HTTPCache) responseMetaPath(key string) string {
	return httpCache.storagePath("responses", key+".json")
}

func (httpCache *HTTPCache) responseBinaryPath(key string) string {
	return httpCache.storagePath("responses", key+".bin")
}

func (httpCache *HTTPCache) readResponseRecord(key string) (*cachedResponseRecord, error) {
	raw, err := httpCache.medium.Read(httpCache.responseMetaPath(key))
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, core.E("cache.HTTPCache.readResponseRecord", "failed to read cached response", err)
	}

	var envelope map[string]rawJSON
	envelopeResult := core.JSONUnmarshalString(raw, &envelope)
	if !envelopeResult.OK {
		return nil, core.E("cache.HTTPCache.readResponseRecord", "failed to unmarshal cached response", envelopeResult.Value.(error))
	}

	_, hasRequest := envelope["request"]
	_, hasResponse := envelope["response"]

	if hasRequest || hasResponse {
		if !hasRequest || !hasResponse {
			return nil, core.E("cache.HTTPCache.readResponseRecord", "cached response envelope is incomplete", nil)
		}

		var record cachedResponseRecord
		recordResult := core.JSONUnmarshalString(raw, &record)
		if !recordResult.OK {
			return nil, core.E("cache.HTTPCache.readResponseRecord", "failed to unmarshal cached response", recordResult.Value.(error))
		}
		if err := validateCachedResponseRecord(key, &record); err != nil {
			return nil, err
		}
		return &record, nil
	}

	var record cachedResponseRecord
	var response CachedResponse
	responseResult := core.JSONUnmarshalString(raw, &response)
	if !responseResult.OK {
		return nil, core.E("cache.HTTPCache.readResponseRecord", "failed to unmarshal cached response", responseResult.Value.(error))
	}

	req, err := decodeRequestKey(key)
	if err != nil {
		return nil, err
	}

	record = cachedResponseRecord{
		Request:  req,
		Response: response,
	}
	if err := validateCachedResponseRecord(key, &record); err != nil {
		return nil, err
	}

	return &record, nil
}

// Match finds a cached response for request.
//
//	resp, err := cache.Match(cache.CachedRequest{URL: "https://x", Method: "GET"})
func (httpCache *HTTPCache) Match(req CachedRequest) (*CachedResponse, error) {
	if err := httpCache.ensureReady("cache.HTTPCache.Match"); err != nil {
		return nil, err
	}
	if err := validateCachedRequest(req); err != nil {
		return nil, core.E("cache.HTTPCache.Match", "invalid cached request", err)
	}
	key, err := httpCache.requestKey(req)
	if err != nil {
		return nil, err
	}

	record, err := httpCache.readResponseRecord(key)
	if err != nil {
		return nil, err
	}
	if record == nil {
		record, err = httpCache.readResponseRecord(legacyRequestKey(req))
	}
	if err != nil || record == nil {
		return nil, err
	}
	return &record.Response, nil
}

// Put stores a request/response pair and its body.
//
//	err := appCache.Put(
//	    cache.CachedRequest{URL: "https://example.com/style.css", Method: "GET"},
//	    cache.CachedResponse{Status: 200, Headers: headers},
//	    bodyBytes,
//	)
func (httpCache *HTTPCache) Put(req CachedRequest, resp CachedResponse, body []byte) error {
	if err := httpCache.ensureReady("cache.HTTPCache.Put"); err != nil {
		return err
	}
	key, err := httpCache.requestKey(req)
	if err != nil {
		return err
	}
	resp.BodyPath = core.JoinPath("responses", key+".bin")
	if err := validateCachedRequest(req); err != nil {
		return core.E("cache.HTTPCache.Put", "invalid cached request", err)
	}
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}
	if err := validateCachedResponse(resp); err != nil {
		return core.E("cache.HTTPCache.Put", "invalid cached response", err)
	}

	if err := httpCache.medium.EnsureDir(httpCache.storagePath("responses")); err != nil {
		return core.E("cache.HTTPCache.Put", "failed to create response directory", err)
	}

	metaPath := httpCache.responseMetaPath(key)
	binaryPath := httpCache.responseBinaryPath(key)
	metaSnapshot, err := readFileSnapshot(httpCache.medium, metaPath)
	if err != nil {
		return core.E("cache.HTTPCache.Put", "failed to inspect existing cached response metadata", err)
	}
	binarySnapshot, err := readFileSnapshot(httpCache.medium, binaryPath)
	if err != nil {
		return core.E("cache.HTTPCache.Put", "failed to inspect existing cached response body", err)
	}

	resp.CachedAt = time.Now()
	record := cachedResponseRecord{
		Request:  req,
		Response: resp,
	}
	meta, err := marshalPrettyJSON(record)
	if err != nil {
		return core.E("cache.HTTPCache.Put", "failed to marshal cached response", err)
	}

	if err := httpCache.medium.Write(binaryPath, string(body)); err != nil {
		_ = restoreFileSnapshot(httpCache.medium, metaSnapshot)
		_ = restoreFileSnapshot(httpCache.medium, binarySnapshot)
		return core.E("cache.HTTPCache.Put", "failed to write cached response body", err)
	}
	if err := httpCache.medium.Write(metaPath, meta); err != nil {
		_ = restoreFileSnapshot(httpCache.medium, binarySnapshot)
		_ = restoreFileSnapshot(httpCache.medium, metaSnapshot)
		return core.E("cache.HTTPCache.Put", "failed to write cached response metadata", err)
	}

	return nil
}

// ReadBody returns the response body bytes from medium.
//
//	body, err := appCache.ReadBody(resp)
func (httpCache *HTTPCache) ReadBody(resp *CachedResponse) ([]byte, error) {
	if err := httpCache.ensureReady("cache.HTTPCache.ReadBody"); err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, core.E("cache.HTTPCache.ReadBody", "response is nil", nil)
	}
	if resp.BodyPath == "" {
		return nil, core.E("cache.HTTPCache.ReadBody", "response has empty body path", nil)
	}
	if err := ensureSafeResponseBodyPath(resp.BodyPath); err != nil {
		return nil, core.E("cache.HTTPCache.ReadBody", "invalid response body path", err)
	}
	body, err := httpCache.medium.Read(httpCache.storagePath(resp.BodyPath))
	if err != nil {
		return nil, core.E("cache.HTTPCache.ReadBody", "failed to read response body", err)
	}
	return []byte(body), nil
}

func validateCachedResponseRecord(key string, record *cachedResponseRecord) error {
	if record == nil {
		return core.E("cache.HTTPCache.validateCachedResponseRecord", "cached response record is nil", nil)
	}

	if err := validateCachedRequest(record.Request); err != nil {
		return core.E("cache.HTTPCache.validateCachedResponseRecord", "invalid cached request", err)
	}

	expectedKey, err := requestStorageKey(record.Request)
	if err != nil {
		return err
	}
	legacyKey := legacyRequestKey(record.Request)
	if key != expectedKey && key != legacyKey {
		return core.E("cache.HTTPCache.validateCachedResponseRecord", "cached request metadata does not match cache key", nil)
	}

	if err := validateCachedResponse(record.Response); err != nil {
		return err
	}
	expectedBodyPaths := []string{
		core.JoinPath("responses", expectedKey+".bin"),
		core.JoinPath("responses", legacyKey+".bin"),
	}
	if !slices.Contains(expectedBodyPaths, record.Response.BodyPath) {
		return core.E("cache.HTTPCache.validateCachedResponseRecord", "cached response body path does not match cache key", nil)
	}

	return nil
}

func requestStorageKey(req CachedRequest) (string, error) {
	if err := validateCachedRequest(req); err != nil {
		return "", core.E("cache.HTTPCache.requestStorageKey", "invalid cached request", err)
	}

	return core.SHA256Hex([]byte(req.Method + "\x00" + req.URL)), nil
}

func validateCachedRequest(req CachedRequest) error {
	if core.Trim(req.URL) == "" || core.Trim(req.Method) == "" {
		return core.E("cache.HTTPCache.validateCachedRequest", "request URL and method are required", nil)
	}
	if len(req.URL) > maxCachedRequestURLBytes {
		return core.E("cache.HTTPCache.validateCachedRequest", "request URL is too long", nil)
	}
	if len(req.Method) > maxCachedRequestMethodBytes {
		return core.E("cache.HTTPCache.validateCachedRequest", "request method is too long", nil)
	}
	if hasHTTPDangerousBytes(req.URL) || hasHTTPDangerousBytes(req.Method) {
		return core.E("cache.HTTPCache.validateCachedRequest", "request contains control characters", nil)
	}
	if !isHTTPToken(req.Method) {
		return core.E("cache.HTTPCache.validateCachedRequest", "invalid HTTP method", nil)
	}
	return nil
}

func validateCachedResponse(resp CachedResponse) error {
	if resp.Status < 100 || resp.Status > 599 {
		return core.E("cache.HTTPCache.validateCachedResponse", "invalid HTTP status", nil)
	}
	if hasHTTPDangerousBytes(resp.StatusText) {
		return core.E("cache.HTTPCache.validateCachedResponse", "invalid HTTP status text", nil)
	}
	if len(resp.StatusText) > maxCachedStatusTextBytes {
		return core.E("cache.HTTPCache.validateCachedResponse", "HTTP status text is too long", nil)
	}
	if err := ensureSafeResponseBodyPath(resp.BodyPath); err != nil {
		return core.E("cache.HTTPCache.validateCachedResponse", "invalid response body path", err)
	}
	if len(resp.Headers) > maxCachedHeaderCount {
		return core.E("cache.HTTPCache.validateCachedResponse", "too many response headers", nil)
	}
	for name, value := range resp.Headers {
		if len(name) > maxCachedHeaderNameBytes {
			return core.E("cache.HTTPCache.validateCachedResponse", "response header name is too long", nil)
		}
		if len(value) > maxCachedHeaderValueBytes {
			return core.E("cache.HTTPCache.validateCachedResponse", "response header value is too long", nil)
		}
		if err := validateHTTPHeaderName(name); err != nil {
			return core.E("cache.HTTPCache.validateCachedResponse", "invalid response header name", err)
		}
		if hasHTTPDangerousBytes(value) {
			return core.E("cache.HTTPCache.validateCachedResponse", "invalid response header value", nil)
		}
	}
	return nil
}

func validateHTTPHeaderName(name string) error {
	if name == "" {
		return core.E("cache.HTTPCache.validateHTTPHeaderName", "header name is empty", nil)
	}
	if !isHTTPToken(name) {
		return core.E("cache.HTTPCache.validateHTTPHeaderName", "invalid header name", nil)
	}
	return nil
}

func hasHTTPDangerousBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func isHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
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
//
//	err := appCache.Delete(cache.CachedRequest{URL: "https://example.com/old.js", Method: "GET"})
func (httpCache *HTTPCache) Delete(req CachedRequest) error {
	if err := httpCache.ensureReady("cache.HTTPCache.Delete"); err != nil {
		return err
	}
	if err := validateCachedRequest(req); err != nil {
		return core.E("cache.HTTPCache.Delete", "invalid cached request", err)
	}

	key, err := httpCache.requestKey(req)
	if err != nil {
		return err
	}

	if err := httpCache.medium.Delete(httpCache.responseMetaPath(key)); err != nil && !core.Is(err, fs.ErrNotExist) {
		return core.E("cache.HTTPCache.Delete", "failed to delete cached response metadata", err)
	}
	if err := httpCache.medium.Delete(httpCache.responseBinaryPath(key)); err != nil && !core.Is(err, fs.ErrNotExist) {
		return core.E("cache.HTTPCache.Delete", "failed to delete cached response body", err)
	}
	legacyKey := legacyRequestKey(req)
	if legacyKey != key {
		if err := httpCache.medium.Delete(httpCache.responseMetaPath(legacyKey)); err != nil && !core.Is(err, fs.ErrNotExist) {
			return core.E("cache.HTTPCache.Delete", "failed to delete legacy cached response metadata", err)
		}
		if err := httpCache.medium.Delete(httpCache.responseBinaryPath(legacyKey)); err != nil && !core.Is(err, fs.ErrNotExist) {
			return core.E("cache.HTTPCache.Delete", "failed to delete legacy cached response body", err)
		}
	}

	return nil
}

// Keys returns all cached request URLs.
//
//	urls, err := appCache.Keys()
//	// ["https://example.com/style.css", "https://example.com/app.js"]
func (httpCache *HTTPCache) Keys() ([]string, error) {
	if err := httpCache.ensureReady("cache.HTTPCache.Keys"); err != nil {
		return nil, err
	}

	entries, err := httpCache.medium.List(httpCache.storagePath("responses"))
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, core.E("cache.HTTPCache.Keys", "failed to list response entries", err)
	}

	seen := make(map[string]struct{})
	var urls []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !core.HasSuffix(name, ".json") {
			continue
		}
		key := core.TrimSuffix(name, ".json")
		record, err := httpCache.readResponseRecord(key)
		if err != nil {
			continue
		}
		if record == nil || record.Request.URL == "" {
			continue
		}
		if _, ok := seen[record.Request.URL]; ok {
			continue
		}
		seen[record.Request.URL] = struct{}{}
		urls = append(urls, record.Request.URL)
	}

	slices.Sort(urls)
	return urls, nil
}

type fileSnapshot struct {
	path    string
	existed bool
	content string
}

func readFileSnapshot(medium coreio.Medium, path string) (fileSnapshot, error) {
	content, err := medium.Read(path)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return fileSnapshot{path: path}, nil
		}
		return fileSnapshot{}, err
	}
	return fileSnapshot{
		path:    path,
		existed: true,
		content: content,
	}, nil
}

func restoreFileSnapshot(medium coreio.Medium, snapshot fileSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.existed {
		if err := medium.Delete(snapshot.path); err != nil && !core.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return medium.Write(snapshot.path, snapshot.content)
}

// Clear removes all cached items under the cache base directory.
//
//	err := c.Clear()
func (c *Cache) Clear() error {
	if err := c.ensureReady("cache.Clear"); err != nil {
		return err
	}

	if err := c.medium.DeleteAll(c.baseDir); err != nil {
		return core.E("cache.Clear", "failed to clear cache", err)
	}
	return nil
}

// Age reports how long ago key was cached, or -1 if it is missing or unreadable.
//
//	age := c.Age("github/acme/repos")
func (c *Cache) Age(key string) time.Duration {
	if err := c.ensureReady("cache.Age"); err != nil {
		return -1
	}

	path, err := c.Path(key)
	if err != nil {
		return -1
	}

	dataStr, err := c.medium.Read(path)
	if err != nil {
		return -1
	}

	var entry Entry
	entryResult := core.JSONUnmarshalString(dataStr, &entry)
	if !entryResult.OK {
		return -1
	}

	return time.Since(entry.CachedAt)
}

// GitHub-specific cache keys

// GitHubReposKey returns the cache key used for an organisation's repo list.
//
//	key := cache.GitHubReposKey("acme")
func GitHubReposKey(org string) string {
	return core.JoinPath("github", encodePathSegment(org), "repos")
}

// GitHubRepoKey returns the cache key used for a repository metadata entry.
//
//	key := cache.GitHubRepoKey("acme", "widgets")
func GitHubRepoKey(org, repo string) string {
	return core.JoinPath("github", encodePathSegment(org), encodePathSegment(repo), "meta")
}

func encodePathSegment(segment string) string {
	return core.URLPathEscape(segment)
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
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		return normalizePath(cwd)
	}

	cwd := normalizePath(core.Env("PWD"))
	if cwd != "" && cwd != "." {
		return cwd
	}

	return normalizePath(core.Env("DIR_CWD"))
}

func (c *Cache) ensureConfigured(op string) error {
	if c == nil {
		return core.E(op, "cache is nil", nil)
	}
	if c.baseDir == "" {
		return core.E(op, "cache base directory is empty; construct with cache.New", nil)
	}
	if c.runtime == nil {
		return core.E(op, "cache runtime is nil; construct with cache.New", nil)
	}

	return nil
}

func (c *Cache) ensureReady(op string) error {
	if err := c.ensureConfigured(op); err != nil {
		return err
	}
	if c.medium == nil {
		return core.E(op, "cache medium is nil; construct with cache.New", nil)
	}

	return nil
}
