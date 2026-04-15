// SPDX-License-Identifier: EUPL-1.2

package cache_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"dappco.re/go/cache"
	"dappco.re/go/core"
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
	t.Setenv("PWD", "")
	t.Setenv("DIR_CWD", "")

	c, m := newTestCache(t, "", 0)

	const key = "defaults"
	if err := c.Set(key, map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	path, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	wantPath := core.JoinPath(cwd, ".core", "cache", key+".json")
	if path != wantPath {
		t.Fatalf("expected default path %q, got %q", wantPath, path)
	}

	raw, err := m.Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !strings.Contains(raw, "\n  \"data\":") {
		t.Fatalf("expected pretty-printed cache entry, got %q", raw)
	}

	entry := readEntry(t, raw)
	ttl := entry.ExpiresAt.Sub(entry.CachedAt)
	if ttl < cache.DefaultTTL || ttl > cache.DefaultTTL+time.Second {
		t.Fatalf("expected ttl near %v, got %v", cache.DefaultTTL, ttl)
	}
}

func TestCache_New_Bad(t *testing.T) {
	_, err := cache.New(coreio.NewMockMedium(), "/tmp/cache-negative-ttl", -time.Second)
	if err == nil {
		t.Fatal("expected New to reject negative ttl, got nil")
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

func TestCache_NilReceiver_Good(t *testing.T) {
	var c *cache.Cache
	var target map[string]string

	if _, err := c.Path("x"); err == nil {
		t.Fatal("expected Path to fail on nil receiver")
	}

	if _, err := c.Get("x", &target); err == nil {
		t.Fatal("expected Get to fail on nil receiver")
	}

	if err := c.Set("x", map[string]string{"foo": "bar"}); err == nil {
		t.Fatal("expected Set to fail on nil receiver")
	}

	if err := c.Delete("x"); err == nil {
		t.Fatal("expected Delete to fail on nil receiver")
	}

	if err := c.Clear(); err == nil {
		t.Fatal("expected Clear to fail on nil receiver")
	}

	if age := c.Age("x"); age != -1 {
		t.Fatalf("expected Age to return -1 on nil receiver, got %v", age)
	}
}

func TestCache_ZeroValue_Ugly(t *testing.T) {
	var c cache.Cache
	var target map[string]string

	if _, err := c.Path("x"); err == nil {
		t.Fatal("expected Path to fail on zero-value cache")
	}

	if _, err := c.Get("x", &target); err == nil {
		t.Fatal("expected Get to fail on zero-value cache")
	}

	if err := c.Set("x", map[string]string{"foo": "bar"}); err == nil {
		t.Fatal("expected Set to fail on zero-value cache")
	}

	if err := c.Delete("x"); err == nil {
		t.Fatal("expected Delete to fail on zero-value cache")
	}

	if err := c.Clear(); err == nil {
		t.Fatal("expected Clear to fail on zero-value cache")
	}

	if age := c.Age("x"); age != -1 {
		t.Fatalf("expected Age to return -1 on zero-value cache, got %v", age)
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

func TestCache_DeleteMany_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-delete-many", time.Minute)
	data := map[string]string{"foo": "bar"}

	if err := c.Set("key1", data); err != nil {
		t.Fatalf("Set failed for key1: %v", err)
	}
	if err := c.Set("key2", data); err != nil {
		t.Fatalf("Set failed for key2: %v", err)
	}
	if err := c.DeleteMany("key1", "missing", "key2"); err != nil {
		t.Fatalf("DeleteMany failed: %v", err)
	}

	var retrieved map[string]string
	found, err := c.Get("key1", &retrieved)
	if err != nil {
		t.Fatalf("Get after DeleteMany returned an unexpected error: %v", err)
	}
	if found {
		t.Error("expected key1 to be deleted")
	}

	found, err = c.Get("key2", &retrieved)
	if err != nil {
		t.Fatalf("Get after DeleteMany returned an unexpected error: %v", err)
	}
	if found {
		t.Error("expected key2 to be deleted")
	}
}

func TestCache_DeleteMany_RejectsTraversalBeforeDeletingAnything(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-delete-many-traversal", time.Minute)

	if err := c.Set("key1", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set failed for key1: %v", err)
	}
	if err := c.Set("key2", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Set failed for key2: %v", err)
	}

	if err := c.DeleteMany("key1", "../../etc/passwd", "key2"); err == nil {
		t.Fatal("expected DeleteMany to reject traversal key")
	}

	var retrieved map[string]string
	found, err := c.Get("key1", &retrieved)
	if err != nil {
		t.Fatalf("Get after rejected DeleteMany returned an unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected key1 to remain after rejected DeleteMany")
	}

	found, err = c.Get("key2", &retrieved)
	if err != nil {
		t.Fatalf("Get after rejected DeleteMany returned an unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected key2 to remain after rejected DeleteMany")
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

func TestCache_GitHubReposKey_EscapesUnsafeSegments(t *testing.T) {
	key := cache.GitHubReposKey("my/org")
	if key != "github/my%2Forg/repos" {
		t.Fatalf("unexpected escaped GitHubReposKey: %q", key)
	}
}

func TestCache_GitHubRepoKey_Good(t *testing.T) {
	key := cache.GitHubRepoKey("myorg", "myrepo")
	if key != "github/myorg/myrepo/meta" {
		t.Errorf("unexpected GitHubRepoKey: %q", key)
	}
}

func TestCache_GitHubRepoKey_EscapesUnsafeSegments(t *testing.T) {
	key := cache.GitHubRepoKey("my/org", "widgets/v2")
	if key != "github/my%2Forg/widgets%2Fv2/meta" {
		t.Fatalf("unexpected escaped GitHubRepoKey: %q", key)
	}
}

func TestCache_SetWithTTL_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-set-with-ttl", 10*time.Minute)

	key := "session/short"
	err := c.SetWithTTL(key, map[string]string{"token": "abc"}, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("SetWithTTL failed: %v", err)
	}

	var dest map[string]string
	found, err := c.Get(key, &dest)
	if err != nil {
		t.Fatalf("Get before expiry failed: %v", err)
	}
	if !found {
		t.Fatalf("expected key before expiry")
	}
	if dest["token"] != "abc" {
		t.Fatalf("expected token=abc, got %q", dest["token"])
	}

	time.Sleep(35 * time.Millisecond)
	found, err = c.Get(key, &dest)
	if err != nil {
		t.Fatalf("Get after expiry failed: %v", err)
	}
	if found {
		t.Fatalf("expected key to expire")
	}
}

func TestCache_SetWithTTL_ZeroExpiresImmediately(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-set-with-ttl-zero", 10*time.Minute)

	key := "session/instant"
	if err := c.SetWithTTL(key, map[string]string{"token": "abc"}, 0); err != nil {
		t.Fatalf("SetWithTTL failed: %v", err)
	}

	var dest map[string]string
	found, err := c.Get(key, &dest)
	if err != nil {
		t.Fatalf("Get after zero ttl failed: %v", err)
	}
	if found {
		t.Fatalf("expected zero ttl entry to expire immediately")
	}
}

func TestCache_Binary_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary", 10*time.Minute)

	blob := []byte{0x00, 0x01, 0x02, 0x03}
	err := c.SetBinary("wasm/my-module", blob, "application/wasm")
	if err != nil {
		t.Fatalf("SetBinary failed: %v", err)
	}

	data, found, err := c.GetBinary("wasm/my-module")
	if err != nil {
		t.Fatalf("GetBinary failed: %v", err)
	}
	if !found {
		t.Fatalf("expected binary data")
	}
	if string(data) != string(blob) {
		t.Fatalf("unexpected binary payload: %q", data)
	}
}

func TestCache_Binary_RoundTripArbitraryBytes(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary-arbitrary", 10*time.Minute)

	blob := []byte{0x00, 0x7f, 0x80, 0xff, 0x1b}
	if err := c.SetBinary("wasm/opaque", blob, "application/octet-stream"); err != nil {
		t.Fatalf("SetBinary failed: %v", err)
	}

	data, found, err := c.GetBinary("wasm/opaque")
	if err != nil {
		t.Fatalf("GetBinary failed: %v", err)
	}
	if !found {
		t.Fatalf("expected binary data")
	}
	if len(data) != len(blob) {
		t.Fatalf("unexpected payload length: got %d want %d", len(data), len(blob))
	}
	for i := range blob {
		if data[i] != blob[i] {
			t.Fatalf("unexpected byte at %d: got 0x%x want 0x%x", i, data[i], blob[i])
		}
	}
}

func TestCache_Binary_WithTTL_Expires(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary-expiry", 10*time.Minute)

	blob := []byte("temporary")
	if err := c.SetBinaryWithTTL("temp/nonce", blob, "text/plain", 10*time.Millisecond); err != nil {
		t.Fatalf("SetBinaryWithTTL failed: %v", err)
	}

	time.Sleep(25 * time.Millisecond)
	_, found, err := c.GetBinary("temp/nonce")
	if err != nil {
		t.Fatalf("GetBinary failed: %v", err)
	}
	if found {
		t.Fatalf("expected binary item to expire")
	}
}

func TestCache_Binary_WithTTL_ZeroExpiresImmediately(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary-zero-expiry", 10*time.Minute)

	blob := []byte("instant")
	if err := c.SetBinaryWithTTL("temp/instant", blob, "text/plain", 0); err != nil {
		t.Fatalf("SetBinaryWithTTL failed: %v", err)
	}

	_, found, err := c.GetBinary("temp/instant")
	if err != nil {
		t.Fatalf("GetBinary after zero ttl failed: %v", err)
	}
	if found {
		t.Fatalf("expected zero ttl binary entry to expire immediately")
	}
}

func TestCache_PublicMethods_RejectTraversalKeys(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-traversal-coverage", time.Minute)

	if err := c.SetWithTTL("../../etc/passwd", "value", time.Second); err == nil {
		t.Fatal("expected SetWithTTL to reject traversal key")
	}

	if err := c.SetBinary("../../etc/passwd", []byte("blob"), "text/plain"); err == nil {
		t.Fatal("expected SetBinary to reject traversal key")
	}

	if _, found, err := c.GetBinary("../../etc/passwd"); err == nil || found {
		t.Fatalf("expected GetBinary to reject traversal key, found=%v err=%v", found, err)
	}
}

func TestCache_Scoped_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-scoped", time.Minute)

	app := c.Scoped("https://app.example.com")
	admin := c.Scoped("https://admin.example.com")

	if err := app.Set("user/profile", "app-user"); err != nil {
		t.Fatalf("app Set failed: %v", err)
	}
	if err := admin.Set("user/profile", "admin-user"); err != nil {
		t.Fatalf("admin Set failed: %v", err)
	}

	var appVal string
	var adminVal string

	found, err := app.Get("user/profile", &appVal)
	if err != nil || !found || appVal != "app-user" {
		t.Fatalf("unexpected app scoped value: found=%v val=%q err=%v", found, appVal, err)
	}

	found, err = admin.Get("user/profile", &adminVal)
	if err != nil || !found || adminVal != "admin-user" {
		t.Fatalf("unexpected admin scoped value: found=%v val=%q err=%v", found, adminVal, err)
	}

	if err := c.ClearScope("https://app.example.com"); err != nil {
		t.Fatalf("ClearScope failed: %v", err)
	}

	found, err = app.Get("user/profile", &appVal)
	if err != nil || found {
		t.Fatalf("expected app scope to be cleared, found=%v err=%v", found, err)
	}
	found, err = admin.Get("user/profile", &adminVal)
	if err != nil || !found {
		t.Fatalf("expected admin scope to remain, found=%v err=%v", found, err)
	}
}

func TestCache_Scoped_ClearScope_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-scoped-clear-scope", time.Minute)

	app := c.Scoped("https://app.example.com")
	admin := c.Scoped("https://admin.example.com")

	if err := app.Set("user/profile", "app-user"); err != nil {
		t.Fatalf("app Set failed: %v", err)
	}
	if err := admin.Set("user/profile", "admin-user"); err != nil {
		t.Fatalf("admin Set failed: %v", err)
	}

	if err := app.ClearScope("https://app.example.com"); err != nil {
		t.Fatalf("scoped ClearScope failed: %v", err)
	}

	var appVal string
	var adminVal string

	found, err := app.Get("user/profile", &appVal)
	if err != nil || found {
		t.Fatalf("expected app scope to be cleared, found=%v err=%v", found, err)
	}

	found, err = admin.Get("user/profile", &adminVal)
	if err != nil || !found || adminVal != "admin-user" {
		t.Fatalf("expected admin scope to remain, found=%v val=%q err=%v", found, adminVal, err)
	}
}

func TestCache_Invalidate_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-invalidate", time.Minute)

	if err := c.Set("dns/example.com/A", map[string]string{"a": "1"}); err != nil {
		t.Fatalf("Set dns entry failed: %v", err)
	}
	if err := c.Set("dns/example.com/sub/path", map[string]string{"a": "2"}); err != nil {
		t.Fatalf("Set nested dns entry failed: %v", err)
	}
	if err := c.Set("config/theme", "dark"); err != nil {
		t.Fatalf("Set config entry failed: %v", err)
	}

	c.OnInvalidate("dns.tree-root-changed", func(trigger string) []string {
		return []string{"dns/*"}
	})
	deleted, err := c.Invalidate("dns.tree-root-changed")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expected at least one deleted entry")
	}

	var dnsValue map[string]string
	found, err := c.Get("dns/example.com/A", &dnsValue)
	if err != nil {
		t.Fatalf("Get after invalidation failed: %v", err)
	}
	if found {
		t.Fatal("expected dns entry to be deleted")
	}
	found, err = c.Get("dns/example.com/sub/path", &dnsValue)
	if err != nil {
		t.Fatalf("Get nested dns entry after invalidation failed: %v", err)
	}
	if found {
		t.Fatal("expected nested dns entry to be deleted")
	}
	var theme string
	found, err = c.Get("config/theme", &theme)
	if err != nil || !found {
		t.Fatalf("expected config entry to remain, found=%v err=%v", found, err)
	}
}

func TestCache_HTTPCacheStorage_RejectsTraversalNames(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-traversal")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	if _, err := storage.Open("../evil"); err == nil {
		t.Fatal("expected Open to reject traversal cache name")
	}

	if err := storage.Delete("../evil"); err == nil {
		t.Fatal("expected Delete to reject traversal cache name")
	}
}

func TestCache_HTTPCacheStorage_Good(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("my-app-v1")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	resp := cache.CachedResponse{
		Status:     200,
		StatusText: "OK",
		Headers: map[string]string{
			"Content-Type": "text/css",
		},
	}

	if err := httpCache.Put(req, resp, []byte("body")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	matched, err := httpCache.Match(req)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if matched == nil {
		t.Fatalf("expected matched response")
	}

	body, err := httpCache.ReadBody(matched)
	if err != nil {
		t.Fatalf("ReadBody failed: %v", err)
	}
	if string(body) != "body" {
		t.Fatalf("unexpected body: %q", body)
	}

	urls, err := httpCache.Keys()
	if err != nil {
		t.Fatalf("Keys failed: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected one URL, got %d", len(urls))
	}
	if urls[0] != "https://example.com/style.css" {
		t.Fatalf("unexpected url: %q", urls[0])
	}

	if err := httpCache.Delete(req); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	matched, err = httpCache.Match(req)
	if err != nil {
		t.Fatalf("Match after delete failed: %v", err)
	}
	if matched != nil {
		t.Fatalf("expected response to be deleted")
	}

	names, err := storage.Keys()
	if err != nil {
		t.Fatalf("storage.Keys before delete failed: %v", err)
	}
	if len(names) != 1 || names[0] != "my-app-v1" {
		t.Fatalf("expected cache name to be listed, got %v", strings.Join(names, ","))
	}

	if err := storage.Delete("my-app-v1"); err != nil {
		t.Fatalf("storage.Delete failed: %v", err)
	}

	if err := storage.Delete("my-app-v1"); err != nil {
		t.Fatalf("storage.Delete on missing cache should be a no-op, got %v", err)
	}

	names, err = storage.Keys()
	if err != nil {
		t.Fatalf("storage.Keys failed: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected cache name removed, got %v", strings.Join(names, ","))
	}
}

func TestCache_HTTPCacheStorage_Close_Good(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-close")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	if err := storage.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestCache_HTTPCacheStorage_DottedName_Good(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-dotted")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("api.v2-cache")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/api",
		Method: "GET",
	}
	resp := cache.CachedResponse{Status: 200, StatusText: "OK"}

	if err := httpCache.Put(req, resp, []byte("ok")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	names, err := storage.Keys()
	if err != nil {
		t.Fatalf("storage.Keys failed: %v", err)
	}
	if len(names) != 1 || names[0] != "api.v2-cache" {
		t.Fatalf("expected dotted cache name to be listed, got %v", strings.Join(names, ","))
	}
}

func TestCache_HTTPCacheDeleteMissing_Good(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-delete-missing")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("missing-delete")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/missing.js",
		Method: "GET",
	}

	if err := httpCache.Delete(req); err != nil {
		t.Fatalf("Delete on missing request should be a no-op, got %v", err)
	}
}

func TestCache_HTTPCacheReadBody_Bad(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-body-safety")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("body-safety")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	if _, err := httpCache.ReadBody(&cache.CachedResponse{BodyPath: "../../etc/passwd"}); err == nil {
		t.Fatal("expected ReadBody to reject traversal body paths")
	}
}
