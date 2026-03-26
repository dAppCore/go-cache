// SPDX-License-Identifier: EUPL-1.2

// Package cache provides a storage-agnostic, JSON-based cache backed by any io.Medium.
package cache

import (
	"encoding/json"
	"os"
	"time"

	"dappco.re/go/core"
	coreio "dappco.re/go/core/io"
	coreerr "dappco.re/go/core/log"
)

// DefaultTTL is the default cache expiry time.
//
// Usage example:
//
//	c, err := cache.New(coreio.NewMockMedium(), "/tmp/cache", cache.DefaultTTL)
const DefaultTTL = 1 * time.Hour

// Cache represents a file-based cache.
//
// Usage example:
//
//	c, err := cache.New(coreio.NewMockMedium(), "/tmp/cache", time.Minute)
type Cache struct {
	medium  coreio.Medium
	baseDir string
	ttl     time.Duration
}

// Entry represents a cached item with metadata.
//
// Usage example:
//
//	entry := cache.Entry{CachedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
type Entry struct {
	Data      json.RawMessage `json:"data"`
	CachedAt  time.Time       `json:"cached_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// New creates a new cache instance.
// If medium is nil, uses coreio.Local (filesystem).
// If baseDir is empty, uses .core/cache in current directory.
//
// Usage example:
//
//	c, err := cache.New(coreio.NewMockMedium(), "/tmp/cache", 30*time.Minute)
func New(medium coreio.Medium, baseDir string, ttl time.Duration) (*Cache, error) {
	if medium == nil {
		medium = coreio.Local
	}

	if baseDir == "" {
		// Use .core/cache in current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return nil, coreerr.E("cache.New", "failed to get working directory", err)
		}
		baseDir = core.Path(cwd, ".core", "cache")
	}

	if ttl == 0 {
		ttl = DefaultTTL
	}

	// Ensure cache directory exists
	if err := medium.EnsureDir(baseDir); err != nil {
		return nil, coreerr.E("cache.New", "failed to create cache directory", err)
	}

	return &Cache{
		medium:  medium,
		baseDir: baseDir,
		ttl:     ttl,
	}, nil
}

// Path returns the full path for a cache key.
// Returns an error if the key attempts path traversal.
//
// Usage example:
//
//	path, err := c.Path("github/acme/repos")
func (c *Cache) Path(key string) (string, error) {
	path := joinPath(c.baseDir, key+".json")

	// Ensure the resulting path is still within baseDir to prevent traversal attacks
	absBase, err := pathAbs(c.baseDir)
	if err != nil {
		return "", coreerr.E("cache.Path", "failed to get absolute path for baseDir", err)
	}
	absPath, err := pathAbs(path)
	if err != nil {
		return "", coreerr.E("cache.Path", "failed to get absolute path for key", err)
	}

	if !core.HasPrefix(absPath, absBase+pathSeparator()) && absPath != absBase {
		return "", coreerr.E("cache.Path", "invalid cache key: path traversal attempt", nil)
	}

	return path, nil
}

// Get retrieves a cached item if it exists and hasn't expired.
//
// Usage example:
//
//	found, err := c.Get("session/user-42", &dest)
func (c *Cache) Get(key string, dest any) (bool, error) {
	path, err := c.Path(key)
	if err != nil {
		return false, err
	}

	dataStr, err := c.medium.Read(path)
	if err != nil {
		if core.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, coreerr.E("cache.Get", "failed to read cache file", err)
	}

	var entry Entry
	if err := json.Unmarshal([]byte(dataStr), &entry); err != nil {
		// Invalid cache file, treat as miss
		return false, nil
	}

	// Check expiry
	if time.Now().After(entry.ExpiresAt) {
		return false, nil
	}

	// Unmarshal the actual data
	if err := json.Unmarshal(entry.Data, dest); err != nil {
		return false, coreerr.E("cache.Get", "failed to unmarshal cached data", err)
	}

	return true, nil
}

// Set stores an item in the cache.
//
// Usage example:
//
//	err := c.Set("session/user-42", map[string]string{"name": "Ada"})
func (c *Cache) Set(key string, data any) error {
	path, err := c.Path(key)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := c.medium.EnsureDir(core.PathDir(path)); err != nil {
		return coreerr.E("cache.Set", "failed to create directory", err)
	}

	// Marshal the data
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return coreerr.E("cache.Set", "failed to marshal data", err)
	}

	entry := Entry{
		Data:      dataBytes,
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(c.ttl),
	}

	entryBytes, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return coreerr.E("cache.Set", "failed to marshal cache entry", err)
	}

	if err := c.medium.Write(path, string(entryBytes)); err != nil {
		return coreerr.E("cache.Set", "failed to write cache file", err)
	}
	return nil
}

// Delete removes an item from the cache.
//
// Usage example:
//
//	err := c.Delete("session/user-42")
func (c *Cache) Delete(key string) error {
	path, err := c.Path(key)
	if err != nil {
		return err
	}

	err = c.medium.Delete(path)
	if core.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return coreerr.E("cache.Delete", "failed to delete cache file", err)
	}
	return nil
}

// Clear removes all cached items.
//
// Usage example:
//
//	err := c.Clear()
func (c *Cache) Clear() error {
	if err := c.medium.DeleteAll(c.baseDir); err != nil {
		return coreerr.E("cache.Clear", "failed to clear cache", err)
	}
	return nil
}

// Age returns how old a cached item is, or -1 if not cached.
//
// Usage example:
//
//	age := c.Age("session/user-42")
func (c *Cache) Age(key string) time.Duration {
	path, err := c.Path(key)
	if err != nil {
		return -1
	}

	dataStr, err := c.medium.Read(path)
	if err != nil {
		return -1
	}

	var entry Entry
	if err := json.Unmarshal([]byte(dataStr), &entry); err != nil {
		return -1
	}

	return time.Since(entry.CachedAt)
}

// GitHub-specific cache keys

// GitHubReposKey returns the cache key for an org's repo list.
//
// Usage example:
//
//	key := cache.GitHubReposKey("acme")
func GitHubReposKey(org string) string {
	return core.JoinPath("github", org, "repos")
}

// GitHubRepoKey returns the cache key for a specific repo's metadata.
//
// Usage example:
//
//	key := cache.GitHubRepoKey("acme", "widgets")
func GitHubRepoKey(org, repo string) string {
	return core.JoinPath("github", org, repo, "meta")
}

func joinPath(segments ...string) string {
	return normalizePath(core.JoinPath(segments...))
}

func pathAbs(path string) (string, error) {
	path = normalizePath(path)
	if core.PathIsAbs(path) {
		return core.CleanPath(path, pathSeparator()), nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return core.Path(cwd, path), nil
}

func normalizePath(path string) string {
	if pathSeparator() == "/" {
		return path
	}
	return core.Replace(path, "/", pathSeparator())
}

func pathSeparator() string {
	sep := core.Env("DS")
	if sep == "" {
		return "/"
	}
	return sep
}
