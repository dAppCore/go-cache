// Package cache provides a storage-agnostic, JSON-based cache backed by any io.Medium.
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreio "dappco.re/go/core/io"
	coreerr "dappco.re/go/core/log"
)

// DefaultTTL is the default cache expiry time.
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
		baseDir = filepath.Join(cwd, ".core", "cache")
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

// Path returns the storage path used for key and rejects path traversal
// attempts.
func (c *Cache) Path(key string) (string, error) {
	path := filepath.Join(c.baseDir, key+".json")

	// Ensure the resulting path is still within baseDir to prevent traversal attacks
	absBase, err := filepath.Abs(c.baseDir)
	if err != nil {
		return "", coreerr.E("cache.Path", "failed to get absolute path for baseDir", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", coreerr.E("cache.Path", "failed to get absolute path for key", err)
	}

	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return "", coreerr.E("cache.Path", "invalid cache key: path traversal attempt", nil)
	}

	return path, nil
}

// Get unmarshals the cached item into dest if it exists and has not expired.
func (c *Cache) Get(key string, dest any) (bool, error) {
	path, err := c.Path(key)
	if err != nil {
		return false, err
	}

	dataStr, err := c.medium.Read(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

// Set marshals data and stores it in the cache.
func (c *Cache) Set(key string, data any) error {
	path, err := c.Path(key)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := c.medium.EnsureDir(filepath.Dir(path)); err != nil {
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

// Delete removes the cached item for key.
func (c *Cache) Delete(key string) error {
	path, err := c.Path(key)
	if err != nil {
		return err
	}

	err = c.medium.Delete(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return coreerr.E("cache.Delete", "failed to delete cache file", err)
	}
	return nil
}

// Clear removes all cached items under the cache base directory.
func (c *Cache) Clear() error {
	if err := c.medium.DeleteAll(c.baseDir); err != nil {
		return coreerr.E("cache.Clear", "failed to clear cache", err)
	}
	return nil
}

// Age reports how long ago key was cached, or -1 if it is missing or unreadable.
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

// GitHubReposKey returns the cache key used for an organisation's repo list.
func GitHubReposKey(org string) string {
	return filepath.Join("github", org, "repos")
}

// GitHubRepoKey returns the cache key used for a repository metadata entry.
func GitHubRepoKey(org, repo string) string {
	return filepath.Join("github", org, repo, "meta")
}
