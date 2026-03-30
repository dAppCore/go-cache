// SPDX-License-Identifier: EUPL-1.2

// Package cache provides a storage-agnostic, JSON-based cache backed by any io.Medium.
package cache

import (
	"encoding/json"
	"io/fs"
	"time"

	"dappco.re/go/core"
	coreio "dappco.re/go/core/io"
)

// DefaultTTL is the default cache expiry time.
//
// Usage example:
//
//	c, err := cache.New(coreio.NewMockMedium(), "/tmp/cache", cache.DefaultTTL)
const DefaultTTL = 1 * time.Hour

// Cache stores JSON-encoded entries in a Medium-backed cache rooted at baseDir.
type Cache struct {
	medium  coreio.Medium
	baseDir string
	ttl     time.Duration
}

// Entry is the serialized cache record written to the backing Medium.
type Entry struct {
	Data      json.RawMessage `json:"data"`
	CachedAt  time.Time       `json:"cached_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// New creates a cache and applies default Medium, base directory, and TTL values
// when callers pass zero values.
//
//	c, err := cache.New(coreio.Local, "/tmp/cache", time.Hour)
func New(medium coreio.Medium, baseDir string, ttl time.Duration) (*Cache, error) {
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

	if ttl == 0 {
		ttl = DefaultTTL
	}

	if err := medium.EnsureDir(baseDir); err != nil {
		return nil, core.E("cache.New", "failed to create cache directory", err)
	}

	return &Cache{
		medium:  medium,
		baseDir: baseDir,
		ttl:     ttl,
	}, nil
}

// Path returns the storage path used for key and rejects path traversal
// attempts.
//
//	path, err := c.Path("github/acme/repos")
func (c *Cache) Path(key string) (string, error) {
	if c == nil {
		return "", core.E("cache.Path", "cache is nil", nil)
	}

	baseDir := absolutePath(c.baseDir)
	path := absolutePath(core.JoinPath(baseDir, key+".json"))
	pathPrefix := normalizePath(core.Concat(baseDir, pathSeparator()))

	if path != baseDir && !core.HasPrefix(path, pathPrefix) {
		return "", core.E("cache.Path", "invalid cache key: path traversal attempt", nil)
	}

	return path, nil
}

// Get unmarshals the cached item into dest if it exists and has not expired.
//
//	found, err := c.Get("github/acme/repos", &repos)
func (c *Cache) Get(key string, dest any) (bool, error) {
	if c == nil {
		return false, core.E("cache.Get", "cache is nil", nil)
	}

	path, err := c.Path(key)
	if err != nil {
		return false, err
	}

	dataStr, err := c.medium.Read(path)
	if err != nil {
		if core.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, core.E("cache.Get", "failed to read cache file", err)
	}

	var entry Entry
	entryResult := core.JSONUnmarshalString(dataStr, &entry)
	if !entryResult.OK {
		return false, nil
	}

	if time.Now().After(entry.ExpiresAt) {
		return false, nil
	}

	if err := core.JSONUnmarshal(entry.Data, dest); !err.OK {
		return false, core.E("cache.Get", "failed to unmarshal cached data", err.Value.(error))
	}

	return true, nil
}

// Set marshals data and stores it in the cache.
//
//	err := c.Set("github/acme/repos", repos)
func (c *Cache) Set(key string, data any) error {
	if c == nil {
		return core.E("cache.Set", "cache is nil", nil)
	}

	path, err := c.Path(key)
	if err != nil {
		return err
	}

	if err := c.medium.EnsureDir(core.PathDir(path)); err != nil {
		return core.E("cache.Set", "failed to create directory", err)
	}

	dataResult := core.JSONMarshal(data)
	if !dataResult.OK {
		return core.E("cache.Set", "failed to marshal cache data", dataResult.Value.(error))
	}

	entry := Entry{
		Data:      dataResult.Value.([]byte),
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(c.ttl),
	}

	entryResult := core.JSONMarshal(entry)
	if !entryResult.OK {
		return core.E("cache.Set", "failed to marshal cache entry", entryResult.Value.(error))
	}

	if err := c.medium.Write(path, string(entryResult.Value.([]byte))); err != nil {
		return core.E("cache.Set", "failed to write cache file", err)
	}
	return nil
}

// Delete removes the cached item for key.
//
//	err := c.Delete("github/acme/repos")
func (c *Cache) Delete(key string) error {
	if c == nil {
		return core.E("cache.Delete", "cache is nil", nil)
	}

	path, err := c.Path(key)
	if err != nil {
		return err
	}

	err = c.medium.Delete(path)
	if core.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return core.E("cache.Delete", "failed to delete cache file", err)
	}
	return nil
}

// Clear removes all cached items under the cache base directory.
//
//	err := c.Clear()
func (c *Cache) Clear() error {
	if c == nil {
		return core.E("cache.Clear", "cache is nil", nil)
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
	if c == nil {
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
	return core.JoinPath("github", org, "repos")
}

// GitHubRepoKey returns the cache key used for a repository metadata entry.
//
//	key := cache.GitHubRepoKey("acme", "widgets")
func GitHubRepoKey(org, repo string) string {
	return core.JoinPath("github", org, repo, "meta")
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
	cwd := normalizePath(core.Env("PWD"))
	if cwd != "" && cwd != "." {
		return cwd
	}

	return normalizePath(core.Env("DIR_CWD"))
}
