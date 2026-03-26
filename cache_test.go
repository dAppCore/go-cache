package cache_test

import (
	"testing"
	"time"

	"dappco.re/go/core"
	"dappco.re/go/core/cache"
	coreio "dappco.re/go/core/io"
)

func newTestCache(t *testing.T, baseDir string, ttl time.Duration) (*cache.Cache, *coreio.MockMedium) {
	t.Helper()

	m := coreio.NewMockMedium()
	c, err := cache.New(m, baseDir, ttl)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}

	return c, m
}

func readEntry(t *testing.T, raw string) cache.Entry {
	t.Helper()

	var entry cache.Entry
	result := core.JSONUnmarshalString(raw, &entry)
	if !result.OK {
		t.Fatalf("failed to unmarshal cache entry: %v", result.Value)
	}

	return entry
}

func TestCache_New_Good(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	c, m := newTestCache(t, "", 0)

	const key = "defaults"
	if err := c.Set(key, map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	path, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}

	wantPath := core.JoinPath(tmpDir, ".core", "cache", key+".json")
	if path != wantPath {
		t.Fatalf("expected default path %q, got %q", wantPath, path)
	}

	raw, err := m.Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	entry := readEntry(t, raw)
	ttl := entry.ExpiresAt.Sub(entry.CachedAt)
	if ttl < cache.DefaultTTL || ttl > cache.DefaultTTL+time.Second {
		t.Fatalf("expected ttl near %v, got %v", cache.DefaultTTL, ttl)
	}
}

func TestCache_Path_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-path", time.Minute)

	path, err := c.Path("github/acme/repos")
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}

	want := "/tmp/cache-path/github/acme/repos.json"
	if path != want {
		t.Fatalf("expected path %q, got %q", want, path)
	}
}

func TestCache_Path_Bad(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-traversal", time.Minute)

	_, err := c.Path("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal key, got nil")
	}
}

func TestCache_Get_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache", time.Minute)

	key := "test-key"
	data := map[string]string{"foo": "bar"}

	if err := c.Set(key, data); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	var retrieved map[string]string
	found, err := c.Get(key, &retrieved)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected to find cached item")
	}
	if retrieved["foo"] != "bar" {
		t.Errorf("expected foo=bar, got %v", retrieved["foo"])
	}
}

func TestCache_Get_Ugly(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-expiry", 10*time.Millisecond)

	if err := c.Set("test-key", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set for expiry test failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	var retrieved map[string]string
	found, err := c.Get("test-key", &retrieved)
	if err != nil {
		t.Fatalf("Get for expired item returned an unexpected error: %v", err)
	}
	if found {
		t.Error("expected item to be expired")
	}
}

func TestCache_Age_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-age", time.Minute)

	if err := c.Set("test-key", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if age := c.Age("test-key"); age < 0 {
		t.Errorf("expected age >= 0, got %v", age)
	}
}

func TestCache_Delete_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-delete", time.Minute)

	if err := c.Set("test-key", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if err := c.Delete("test-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var retrieved map[string]string
	found, err := c.Get("test-key", &retrieved)
	if err != nil {
		t.Fatalf("Get after delete returned an unexpected error: %v", err)
	}
	if found {
		t.Error("expected item to be deleted")
	}
}

func TestCache_Clear_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-clear", time.Minute)
	data := map[string]string{"foo": "bar"}

	if err := c.Set("key1", data); err != nil {
		t.Fatalf("Set for clear test failed for key1: %v", err)
	}
	if err := c.Set("key2", data); err != nil {
		t.Fatalf("Set for clear test failed for key2: %v", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	var retrieved map[string]string
	found, err := c.Get("key1", &retrieved)
	if err != nil {
		t.Fatalf("Get after clear returned an unexpected error: %v", err)
	}
	if found {
		t.Error("expected key1 to be cleared")
	}
}

func TestCache_GitHubReposKey_Good(t *testing.T) {
	key := cache.GitHubReposKey("myorg")
	if key != "github/myorg/repos" {
		t.Errorf("unexpected GitHubReposKey: %q", key)
	}
}

func TestCache_GitHubRepoKey_Good(t *testing.T) {
	key := cache.GitHubRepoKey("myorg", "myrepo")
	if key != "github/myorg/myrepo/meta" {
		t.Errorf("unexpected GitHubRepoKey: %q", key)
	}
}
