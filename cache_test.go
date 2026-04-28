// SPDX-License-Identifier: EUPL-1.2

package cache_test

import (
	// Note: AX-6 — test-only, replicates internal key derivation for black-box assertion. Retain.
	"crypto/sha256"
	// Note: AX-6 — test-only, replicates internal key derivation for black-box assertion. Retain.
	"encoding/base64"
	// Note: AX-6 — test-only, replicates internal key derivation for black-box assertion. Retain.
	"encoding/hex"
	// Note: AX-6 — test-only, replicates internal key derivation for black-box assertion. Retain.
	"encoding/json"
	// Note: AX-6 — test-only fs interfaces returned by scriptedMedium and fs.ErrNotExist assertions.
	"io/fs"
	// Note: AX-6 — test-only symlink setup; no core equivalent for os.Symlink.
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "dappco.re/go"
	"dappco.re/go/cache"
	coreio "dappco.re/go/io"
)

type scriptedMedium struct {
	*coreio.MockMedium
	readErr      map[string]error
	writeErr     map[string]error
	ensureDirErr map[string]error
	deleteErr    map[string]error
	deleteAllErr map[string]error
	listErr      map[string]error
}

func newScriptedMedium() *scriptedMedium {
	return &scriptedMedium{
		MockMedium:   coreio.NewMockMedium(),
		readErr:      make(map[string]error),
		writeErr:     make(map[string]error),
		ensureDirErr: make(map[string]error),
		deleteErr:    make(map[string]error),
		deleteAllErr: make(map[string]error),
		listErr:      make(map[string]error),
	}
}

func (m *scriptedMedium) Read(path string) (string, error) {
	if err, ok := m.readErr[path]; ok {
		return "", err
	}
	return m.MockMedium.Read(path)
}

func (m *scriptedMedium) Write(path, content string) error {
	if err, ok := m.writeErr[path]; ok {
		return err
	}
	return m.MockMedium.Write(path, content)
}

func (m *scriptedMedium) WriteMode(path, content string, mode fs.FileMode) error {
	if err, ok := m.writeErr[path]; ok {
		return err
	}
	return m.MockMedium.WriteMode(path, content, mode)
}

func (m *scriptedMedium) EnsureDir(path string) error {
	if err, ok := m.ensureDirErr[path]; ok {
		return err
	}
	return m.MockMedium.EnsureDir(path)
}

func (m *scriptedMedium) Delete(path string) error {
	if err, ok := m.deleteErr[path]; ok {
		return err
	}
	return m.MockMedium.Delete(path)
}

func (m *scriptedMedium) DeleteAll(path string) error {
	if err, ok := m.deleteAllErr[path]; ok {
		return err
	}
	return m.MockMedium.DeleteAll(path)
}

func (m *scriptedMedium) List(path string) ([]fs.DirEntry, error) {
	if err, ok := m.listErr[path]; ok {
		return nil, err
	}
	return m.MockMedium.List(path)
}

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

func httpCacheStorageKey(req cache.CachedRequest) string {
	sum := sha256.Sum256([]byte(req.Method + "\x00" + req.URL))
	return hex.EncodeToString(sum[:])
}

func legacyHTTPCacheStorageKey(req cache.CachedRequest) string {
	return base64.RawURLEncoding.EncodeToString([]byte(req.Method + "\x00" + req.URL))
}

func repeatString(s string, count int) string {
	builder := core.NewBuilder()
	for range count {
		builder.WriteString(s)
	}
	return builder.String()
}

func stableTempDir(t *testing.T) string {
	t.Helper()

	tmpRoot := core.JoinPath(core.Env("DIR_CWD"), ".core", "test-tmp")
	if err := coreio.Local.EnsureDir(tmpRoot); err != nil {
		t.Fatalf("EnsureDir temp root failed: %v", err)
	}
	t.Setenv("TMPDIR", tmpRoot)
	return t.TempDir()
}

func TestCache_New_Good(t *testing.T) {
	tmpDir := stableTempDir(t)
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

	wantPath := core.JoinPath(tmpDir, ".core", "cache", key+".json")
	if path != wantPath {
		t.Fatalf("expected default path %q, got %q", wantPath, path)
	}

	raw, err := m.Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !core.Contains(raw, "\n  \"data\":") {
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

func TestCache_New_Bad_EnsureDirFailure(t *testing.T) {
	medium := newScriptedMedium()
	medium.ensureDirErr["/tmp/cache-new-backend-bad"] = core.E("cache_test", "boom", nil)

	if _, err := cache.New(medium, "/tmp/cache-new-backend-bad", time.Minute); err == nil {
		t.Fatal("expected New to surface backend failure")
	}
}

func TestCache_NewCacheStorage_Good(t *testing.T) {
	tmpDir := stableTempDir(t)
	t.Chdir(tmpDir)
	t.Setenv("PWD", "")
	t.Setenv("DIR_CWD", "")

	storage, err := cache.NewCacheStorage(nil, "")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("assets-v1")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if httpCache == nil {
		t.Fatal("expected Open to return a cache")
	}

	wantDir := core.JoinPath(tmpDir, ".core", "cache-storage", "assets-v1")
	info, err := coreio.Local.Stat(wantDir)
	if err != nil {
		t.Fatalf("expected default cache storage directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", wantDir)
	}
}

func TestCache_NewCacheStorage_Bad(t *testing.T) {
	medium := newScriptedMedium()
	medium.ensureDirErr["/tmp/cache-storage-bad"] = core.E("cache_test", "boom", nil)

	if _, err := cache.NewCacheStorage(medium, "/tmp/cache-storage-bad"); err == nil {
		t.Fatal("expected NewCacheStorage to surface backend failure")
	}
}

func TestCache_SetWithTTL_Bad(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-setwithttl-bad", time.Minute)

	if err := c.SetWithTTL("session/bad", map[string]any{"handler": func() {}}, -time.Second); err == nil {
		t.Fatal("expected SetWithTTL to reject negative ttl")
	}
}

func TestCache_SetWithTTL_Ugly(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-setwithttl-ugly", time.Minute)

	if err := c.SetWithTTL("session/ugly", map[string]any{"handler": func() {}}, time.Second); err == nil {
		t.Fatal("expected SetWithTTL to reject unsupported JSON payload")
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

	tests := []struct {
		name string
		key  string
	}{
		{name: "empty", key: ""},
		{name: "traversal", key: "../../etc/passwd"},
		{name: "dot", key: "."},
		{name: "backslash", key: `foo\bar`},
		{name: "null-byte", key: "foo\x00bar"},
		{name: "too-long", key: repeatString("a", 4097)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Path(tt.key); err == nil {
				t.Fatalf("expected Path to reject %q", tt.key)
			}
		})
	}
}

func TestCache_Path_PathTraversalSymlink_Bad(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := core.JoinPath(tmpDir, "cache")
	outsideDir := core.JoinPath(tmpDir, "outside")
	linkPath := core.JoinPath(baseDir, "link")

	if err := coreio.Local.EnsureDir(baseDir); err != nil {
		t.Fatalf("EnsureDir base failed: %v", err)
	}
	if err := coreio.Local.EnsureDir(outsideDir); err != nil {
		t.Fatalf("EnsureDir outside failed: %v", err)
	}
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	c, err := cache.New(coreio.Local, baseDir, time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if _, err := c.Path("link/escaped"); err == nil {
		t.Fatal("expected Path to reject symlink traversal under baseDir")
	}
	if err := c.Set("link/escaped", "owned"); err == nil {
		t.Fatal("expected Set to reject symlink traversal under baseDir")
	}
	if _, err := coreio.Local.Stat(core.JoinPath(outsideDir, "escaped.json")); err == nil {
		t.Fatal("expected escaped file not to be written outside baseDir")
	} else if !core.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat outside file failed: %v", err)
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

func TestCache_Get_Bad(t *testing.T) {
	c, m := newTestCache(t, "/tmp/cache-get-bad", time.Minute)

	path, err := c.Path("corrupt")
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	m.Files[path] = "{not-json"

	var dest map[string]string
	found, err := c.Get("corrupt", &dest)
	if err == nil {
		t.Fatal("expected Get to reject malformed entry JSON")
	}
	if found {
		t.Fatal("expected malformed entry to be reported as missing")
	}
}

func TestCache_Get_Ugly_MalformedCachedPayload(t *testing.T) {
	c, m := newTestCache(t, "/tmp/cache-get-ugly", time.Minute)

	path, err := c.Path("bad-data")
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}

	entry := cache.Entry{
		Data:      []byte("123"),
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	m.Files[path] = string(raw)

	var dest map[string]string
	found, err := c.Get("bad-data", &dest)
	if err == nil {
		t.Fatal("expected Get to reject malformed cached payload")
	}
	if found {
		t.Fatal("expected malformed payload to be reported as missing")
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

func TestCache_Age_Bad(t *testing.T) {
	c, m := newTestCache(t, "/tmp/cache-age-bad", time.Minute)

	if age := c.Age("missing"); age != -1 {
		t.Fatalf("expected Age to return -1 for missing entry, got %v", age)
	}

	path, err := c.Path("invalid")
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	m.Files[path] = "{not-json"

	if age := c.Age("invalid"); age != -1 {
		t.Fatalf("expected Age to return -1 for malformed entry, got %v", age)
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
	if err := c.SetWithTTL("x", map[string]string{"foo": "bar"}, time.Second); err == nil {
		t.Fatal("expected SetWithTTL to fail on nil receiver")
	}
	if err := c.SetBinary("x", []byte("body"), "text/plain"); err == nil {
		t.Fatal("expected SetBinary to fail on nil receiver")
	}
	if err := c.SetBinaryWithTTL("x", []byte("body"), "text/plain", time.Second); err == nil {
		t.Fatal("expected SetBinaryWithTTL to fail on nil receiver")
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
	if err := c.SetWithTTL("x", map[string]string{"foo": "bar"}, time.Second); err == nil {
		t.Fatal("expected SetWithTTL to fail on zero-value cache")
	}
	if err := c.SetBinary("x", []byte("body"), "text/plain"); err == nil {
		t.Fatal("expected SetBinary to fail on zero-value cache")
	}
	if err := c.SetBinaryWithTTL("x", []byte("body"), "text/plain", time.Second); err == nil {
		t.Fatal("expected SetBinaryWithTTL to fail on zero-value cache")
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

func TestCache_Delete_Bad(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-delete-bad", time.Minute)

	if err := c.Delete("../../etc/passwd"); err == nil {
		t.Fatal("expected Delete to reject traversal key")
	}
}

func TestCache_Delete_Bad_BackendFailure(t *testing.T) {
	medium := newScriptedMedium()
	c, err := cache.New(medium, "/tmp/cache-delete-backend-bad", time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	key := "delete/backend"
	path, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	medium.deleteErr[path] = core.E("cache_test", "boom", nil)

	if err := c.Delete(key); err == nil {
		t.Fatal("expected Delete to surface backend failure")
	}
}

func TestCache_Delete_Ugly(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-delete-ugly", time.Minute)

	if err := c.Delete("missing"); err != nil {
		t.Fatalf("Delete on missing key should be a no-op: %v", err)
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

func TestCache_Clear_Bad(t *testing.T) {
	medium := newScriptedMedium()
	c, err := cache.New(medium, "/tmp/cache-clear-bad", time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	medium.deleteAllErr["/tmp/cache-clear-bad"] = core.E("cache_test", "boom", nil)

	if err := c.Clear(); err == nil {
		t.Fatal("expected Clear to surface backend failure")
	}
}

func TestCache_ClearScope_Bad_ListFailure(t *testing.T) {
	medium := newScriptedMedium()
	c, err := cache.New(medium, "/tmp/cache-clear-scope-bad", time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	medium.listErr["/tmp/cache-clear-scope-bad"] = core.E("cache_test", "boom", nil)

	if err := c.ClearScope("https://app.example.com"); err == nil {
		t.Fatal("expected ClearScope to surface backend list failure")
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

func TestCache_Set_Ugly(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-set-ugly", time.Minute)

	if err := c.Set("bad", func() {}); err == nil {
		t.Fatal("expected Set to reject unsupported JSON payload")
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

func TestCache_SetBinary_Bad(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary-bad", 10*time.Minute)

	if err := c.SetBinaryWithTTL("../../etc/passwd", []byte("blob"), "text/plain", time.Second); err == nil {
		t.Fatal("expected SetBinaryWithTTL to reject traversal key")
	}
}

func TestCache_SetBinaryWithTTL_Bad(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary-negative-ttl", 10*time.Minute)

	if err := c.SetBinaryWithTTL("wasm/negative-ttl", []byte("blob"), "application/wasm", -time.Second); err == nil {
		t.Fatal("expected SetBinaryWithTTL to reject negative ttl")
	}
}

func TestCache_SetBinaryWithTTL_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-binary-with-ttl", 10*time.Minute)

	key := "wasm/ttl"
	blob := []byte("temporary-binary")
	if err := c.SetBinaryWithTTL(key, blob, "application/octet-stream", 20*time.Millisecond); err != nil {
		t.Fatalf("SetBinaryWithTTL failed: %v", err)
	}

	data, found, err := c.GetBinary(key)
	if err != nil {
		t.Fatalf("GetBinary before expiry failed: %v", err)
	}
	if !found {
		t.Fatal("expected binary entry before expiry")
	}
	if string(data) != string(blob) {
		t.Fatalf("unexpected payload: %q", data)
	}

	time.Sleep(35 * time.Millisecond)
	_, found, err = c.GetBinary(key)
	if err != nil {
		t.Fatalf("GetBinary after expiry failed: %v", err)
	}
	if found {
		t.Fatal("expected binary entry to expire")
	}
}

func TestCache_SetBinary_Ugly(t *testing.T) {
	medium := newScriptedMedium()
	c, err := cache.New(medium, "/tmp/cache-binary-ugly", time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	key := "wasm/ugly"
	jsonPath, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	binPath := core.TrimSuffix(jsonPath, ".json") + ".bin"
	medium.writeErr[jsonPath] = core.E("cache_test", "metadata boom", nil)

	if err := c.SetBinary(key, []byte("body"), "application/wasm"); err == nil {
		t.Fatal("expected SetBinary to surface metadata write failure")
	}
	if _, ok := medium.Files[binPath]; ok {
		t.Fatal("expected binary payload to be cleaned up after metadata write failure")
	}
}

func TestCache_SetBinary_Ugly_BinaryWriteFailure(t *testing.T) {
	medium := newScriptedMedium()
	c, err := cache.New(medium, "/tmp/cache-binary-write-failure", time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	key := "wasm/write-failure"
	jsonPath, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	binPath := core.TrimSuffix(jsonPath, ".json") + ".bin"
	medium.writeErr[binPath] = core.E("cache_test", "payload boom", nil)

	if err := c.SetBinary(key, []byte("body"), "application/wasm"); err == nil {
		t.Fatal("expected SetBinary to surface binary write failure")
	}
	if _, ok := medium.Files[jsonPath]; ok {
		t.Fatal("expected metadata to be rolled back after binary write failure")
	}
	if _, ok := medium.Files[binPath]; ok {
		t.Fatal("expected binary payload write to fail without leaving a file behind")
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

func TestCache_GetBinary_Bad(t *testing.T) {
	c, m := newTestCache(t, "/tmp/cache-get-binary-bad", time.Minute)

	if _, found, err := c.GetBinary("missing"); err != nil || found {
		t.Fatalf("expected missing binary entry to be a clean miss, found=%v err=%v", found, err)
	}

	key := "bad/meta"
	metaPath, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	m.Files[metaPath] = "{not-json"

	if _, found, err := c.GetBinary(key); err == nil || found {
		t.Fatalf("expected malformed binary metadata to fail, found=%v err=%v", found, err)
	}
}

func TestCache_GetBinary_Bad_MissingPayload(t *testing.T) {
	c, m := newTestCache(t, "/tmp/cache-get-binary-missing-payload", time.Minute)

	key := "blob/missing"
	if err := c.SetBinary(key, []byte("payload"), "application/octet-stream"); err != nil {
		t.Fatalf("SetBinary failed: %v", err)
	}

	jsonPath, err := c.Path(key)
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	binPath := core.TrimSuffix(jsonPath, ".json") + ".bin"
	delete(m.Files, binPath)

	if data, found, err := c.GetBinary(key); err != nil || found || data != nil {
		t.Fatalf("expected missing payload to be a clean miss, data=%v found=%v err=%v", data, found, err)
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

func TestCache_Scoped_OnInvalidate_ScopesReturnedPatterns(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-scoped-invalidate", time.Minute)

	app := c.Scoped("https://app.example.com")
	admin := c.Scoped("https://admin.example.com")

	if err := app.Set("config/theme", "app-dark"); err != nil {
		t.Fatalf("app Set failed: %v", err)
	}
	if err := admin.Set("config/theme", "admin-dark"); err != nil {
		t.Fatalf("admin Set failed: %v", err)
	}

	app.OnInvalidate("config.changed", func(trigger string) []string {
		return []string{"config/*"}
	})

	deleted, err := app.Invalidate("config.changed")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one scoped entry to be deleted, got %d", deleted)
	}

	var appVal string
	var adminVal string

	found, err := app.Get("config/theme", &appVal)
	if err != nil {
		t.Fatalf("app Get failed: %v", err)
	}
	if found {
		t.Fatalf("expected app scoped config to be deleted")
	}

	found, err = admin.Get("config/theme", &adminVal)
	if err != nil {
		t.Fatalf("admin Get failed: %v", err)
	}
	if !found || adminVal != "admin-dark" {
		t.Fatalf("expected admin scoped config to remain, found=%v val=%q", found, adminVal)
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

func TestCache_Invalidate_UntrustedPatternLength_Bad(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-invalidate-pattern-length", time.Minute)

	if err := c.Set("dns/example.com/A", "record"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	c.OnInvalidate("dns.changed", func(trigger string) []string {
		return []string{repeatString("a", 4097)}
	})

	deleted, err := c.Invalidate("dns.changed")
	if err == nil {
		t.Fatal("expected Invalidate to reject an oversized pattern")
	}
	if deleted != 0 {
		t.Fatalf("expected no deletions after rejecting oversized pattern, got %d", deleted)
	}

	var record string
	found, err := c.Get("dns/example.com/A", &record)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found || record != "record" {
		t.Fatalf("expected entry to remain, found=%v record=%q", found, record)
	}
}

func TestCache_Invalidate_PrefixWildcardDoesNotMatchBarePrefix(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-invalidate-prefix", time.Minute)

	if err := c.Set("dns", "root"); err != nil {
		t.Fatalf("Set bare prefix failed: %v", err)
	}
	if err := c.Set("dns/example.com/A", "record"); err != nil {
		t.Fatalf("Set nested dns entry failed: %v", err)
	}

	c.OnInvalidate("dns.tree-root-changed", func(trigger string) []string {
		return []string{"dns/*"}
	})
	deleted, err := c.Invalidate("dns.tree-root-changed")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one descendant to be deleted, got %d", deleted)
	}

	var root string
	found, err := c.Get("dns", &root)
	if err != nil {
		t.Fatalf("Get bare prefix failed: %v", err)
	}
	if !found || root != "root" {
		t.Fatalf("expected bare prefix entry to remain, found=%v val=%q", found, root)
	}

	var record string
	found, err = c.Get("dns/example.com/A", &record)
	if err != nil {
		t.Fatalf("Get nested entry failed: %v", err)
	}
	if found {
		t.Fatal("expected nested dns entry to be deleted")
	}
}

func TestCache_Invalidate_SingleSegmentWildcard_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-invalidate-segment", time.Minute)

	if err := c.Set("dns/charon.lthn", "one"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := c.Set("dns/charon.local", "two"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := c.Set("dns/other.local", "three"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	c.OnInvalidate("dns.changed", func(trigger string) []string {
		return []string{"dns/charon.*"}
	})

	deleted, err := c.Invalidate("dns.changed")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected two wildcard matches to be deleted, got %d", deleted)
	}

	var value string
	found, err := c.Get("dns/charon.lthn", &value)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("expected charon.lthn to be deleted")
	}
	found, err = c.Get("dns/charon.local", &value)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("expected charon.local to be deleted")
	}
	found, err = c.Get("dns/other.local", &value)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected unrelated entry to remain")
	}
}

func TestCache_OnInvalidate_NilCallbackIsIgnored(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-invalidate-nil", time.Minute)

	if err := c.Set("dns/example.com/A", "record"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	c.OnInvalidate("dns.tree-root-changed", nil)
	deleted, err := c.Invalidate("dns.tree-root-changed")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected nil callback to be ignored, got %d deletions", deleted)
	}

	var record string
	found, err := c.Get("dns/example.com/A", &record)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found || record != "record" {
		t.Fatalf("expected entry to remain, found=%v val=%q", found, record)
	}
}

func TestCache_Scoped_OnInvalidate_NilCallbackIsIgnored(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-scoped-invalidate-nil", time.Minute)

	scoped := c.Scoped("https://app.example.com")

	if err := scoped.Set("dns/example.com/A", "record"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	scoped.OnInvalidate("dns.tree-root-changed", nil)
	deleted, err := scoped.Invalidate("dns.tree-root-changed")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected nil scoped callback to be ignored, got %d deletions", deleted)
	}

	var record string
	found, err := scoped.Get("dns/example.com/A", &record)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found || record != "record" {
		t.Fatalf("expected scoped entry to remain, found=%v val=%q", found, record)
	}
}

func TestCache_Scoped_Wrappers_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-scoped-wrappers", time.Minute)
	scoped := c.Scoped("https://app.example.com")

	if err := scoped.Set("value", "alpha"); err != nil {
		t.Fatalf("Scoped Set failed: %v", err)
	}
	if err := scoped.SetWithTTL("ttl", "beta", 5*time.Millisecond); err != nil {
		t.Fatalf("Scoped SetWithTTL failed: %v", err)
	}
	if err := scoped.SetBinary("blob", []byte("bin"), "application/octet-stream"); err != nil {
		t.Fatalf("Scoped SetBinary failed: %v", err)
	}
	if err := scoped.SetBinaryWithTTL("blob-ttl", []byte("bin2"), "application/octet-stream", 5*time.Millisecond); err != nil {
		t.Fatalf("Scoped SetBinaryWithTTL failed: %v", err)
	}

	path, err := scoped.Path("value")
	if err != nil {
		t.Fatalf("Scoped Path failed: %v", err)
	}
	if !core.Contains(path, "scope_") {
		t.Fatalf("expected scoped path, got %q", path)
	}

	var value string
	found, err := scoped.Get("value", &value)
	if err != nil || !found || value != "alpha" {
		t.Fatalf("unexpected scoped Get result: found=%v value=%q err=%v", found, value, err)
	}

	data, found, err := scoped.GetBinary("blob")
	if err != nil || !found || string(data) != "bin" {
		t.Fatalf("unexpected scoped GetBinary result: found=%v data=%q err=%v", found, data, err)
	}

	if age := scoped.Age("value"); age < 0 {
		t.Fatalf("expected scoped Age >= 0, got %v", age)
	}

	if err := scoped.Delete("value"); err != nil {
		t.Fatalf("Scoped Delete failed: %v", err)
	}
	if err := scoped.DeleteMany("ttl", "blob-ttl"); err != nil {
		t.Fatalf("Scoped DeleteMany failed: %v", err)
	}
	if err := scoped.Clear(); err != nil {
		t.Fatalf("Scoped Clear failed: %v", err)
	}
}

func TestCache_Scoped_Scoped_Good(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-scoped-scoped", time.Minute)

	app := c.Scoped("https://app.example.com")
	admin := app.Scoped("https://admin.example.com")

	if admin == nil {
		t.Fatal("expected Scoped on ScopedCache to return a cache")
	}

	if err := app.Set("user/profile", "app-user"); err != nil {
		t.Fatalf("app Set failed: %v", err)
	}
	if err := admin.Set("user/profile", "admin-user"); err != nil {
		t.Fatalf("admin Set failed: %v", err)
	}

	var appValue string
	found, err := app.Get("user/profile", &appValue)
	if err != nil || !found || appValue != "app-user" {
		t.Fatalf("unexpected app scoped value: found=%v value=%q err=%v", found, appValue, err)
	}

	var adminValue string
	found, err = admin.Get("user/profile", &adminValue)
	if err != nil || !found || adminValue != "admin-user" {
		t.Fatalf("unexpected admin scoped value: found=%v value=%q err=%v", found, adminValue, err)
	}

	if err := admin.Clear(); err != nil {
		t.Fatalf("admin Clear failed: %v", err)
	}

	found, err = app.Get("user/profile", &appValue)
	if err != nil || !found || appValue != "app-user" {
		t.Fatalf("expected app scope to remain after clearing admin, found=%v value=%q err=%v", found, appValue, err)
	}

	found, err = admin.Get("user/profile", &adminValue)
	if err != nil {
		t.Fatalf("admin Get after clear failed: %v", err)
	}
	if found {
		t.Fatal("expected admin scope to be cleared")
	}
}

func TestCache_Scoped_NilReceiver_Bad(t *testing.T) {
	var scoped *cache.ScopedCache
	var dest string

	if scoped.Scoped("https://app.example.com") != nil {
		t.Fatal("expected scoped Scoped to return nil on nil receiver")
	}
	if _, err := scoped.Path("x"); err == nil {
		t.Fatal("expected scoped Path to fail on nil receiver")
	}
	if _, err := scoped.Get("x", &dest); err == nil {
		t.Fatal("expected scoped Get to fail on nil receiver")
	}
	if err := scoped.Set("x", "v"); err == nil {
		t.Fatal("expected scoped Set to fail on nil receiver")
	}
	if err := scoped.SetWithTTL("x", "v", time.Second); err == nil {
		t.Fatal("expected scoped SetWithTTL to fail on nil receiver")
	}
	if err := scoped.SetBinary("x", []byte("v"), "text/plain"); err == nil {
		t.Fatal("expected scoped SetBinary to fail on nil receiver")
	}
	if err := scoped.SetBinaryWithTTL("x", []byte("v"), "text/plain", time.Second); err == nil {
		t.Fatal("expected scoped SetBinaryWithTTL to fail on nil receiver")
	}
	if _, _, err := scoped.GetBinary("x"); err == nil {
		t.Fatal("expected scoped GetBinary to fail on nil receiver")
	}
	if err := scoped.Delete("x"); err == nil {
		t.Fatal("expected scoped Delete to fail on nil receiver")
	}
	if err := scoped.DeleteMany("x"); err == nil {
		t.Fatal("expected scoped DeleteMany to fail on nil receiver")
	}
	if err := scoped.Clear(); err == nil {
		t.Fatal("expected scoped Clear to fail on nil receiver")
	}
	if err := scoped.ClearScope("https://app.example.com"); err == nil {
		t.Fatal("expected scoped ClearScope to fail on nil receiver")
	}
	if _, err := scoped.Invalidate("trigger"); err == nil {
		t.Fatal("expected scoped Invalidate to fail on nil receiver")
	}
	if age := scoped.Age("x"); age != -1 {
		t.Fatalf("expected scoped Age to return -1 on nil receiver, got %v", age)
	}
}

func TestCache_HTTPCacheStorage_RejectsTraversalNames(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-traversal")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "open-empty",
			fn: func() error {
				_, err := storage.Open("")
				return err
			},
		},
		{
			name: "open-dot",
			fn: func() error {
				_, err := storage.Open(".")
				return err
			},
		},
		{
			name: "open-traversal",
			fn: func() error {
				_, err := storage.Open("../evil")
				return err
			},
		},
		{
			name: "delete-backslash",
			fn: func() error {
				return storage.Delete(`bad\cache`)
			},
		},
		{
			name: "open-too-long",
			fn: func() error {
				_, err := storage.Open(repeatString("a", 256))
				return err
			},
		},
		{
			name: "open-newline",
			fn: func() error {
				_, err := storage.Open("cache\nname")
				return err
			},
		},
		{
			name: "open-null-byte",
			fn: func() error {
				_, err := storage.Open("cache\x00name")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err == nil {
				t.Fatalf("expected %s to be rejected", tt.name)
			}
		})
	}
}

func TestCache_HTTPCacheStorage_NilReceiver_Bad(t *testing.T) {
	var storage *cache.CacheStorage

	if _, err := storage.Open("x"); err == nil {
		t.Fatal("expected Open to fail on nil storage")
	}
	if err := storage.Delete("x"); err == nil {
		t.Fatal("expected Delete to fail on nil storage")
	}
	if _, err := storage.Keys(); err == nil {
		t.Fatal("expected Keys to fail on nil storage")
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("Close on nil storage should be a no-op: %v", err)
	}
}

func TestCache_HTTPCacheStorage_Good(t *testing.T) {
	medium := coreio.NewMockMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("my-app-v1")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	if again, err := storage.Open("my-app-v1"); err != nil {
		t.Fatalf("storage.Open reuse failed: %v", err)
	} else if again != httpCache {
		t.Fatal("expected Open to reuse the existing cache instance")
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

	metaEntries, err := medium.List("/tmp/cache-http/my-app-v1/responses")
	if err != nil {
		t.Fatalf("List response metadata failed: %v", err)
	}
	var metaPath string
	for _, entry := range metaEntries {
		if core.HasSuffix(entry.Name(), ".json") {
			metaPath = "/tmp/cache-http/my-app-v1/responses/" + entry.Name()
			break
		}
	}
	if metaPath == "" {
		t.Fatal("expected response metadata file")
	}

	rawMeta, err := medium.Read(metaPath)
	if err != nil {
		t.Fatalf("Read response metadata failed: %v", err)
	}

	var stored struct {
		Request  cache.CachedRequest  `json:"request"`
		Response cache.CachedResponse `json:"response"`
	}
	result := core.JSONUnmarshalString(rawMeta, &stored)
	if !result.OK {
		t.Fatalf("failed to unmarshal stored metadata envelope: %v", result.Value)
	}
	if stored.Request.URL != req.URL || stored.Request.Method != req.Method {
		t.Fatalf("unexpected stored request metadata: %+v", stored.Request)
	}
	if stored.Response.Status != resp.Status || stored.Response.StatusText != resp.StatusText {
		t.Fatalf("unexpected stored response metadata: %+v", stored.Response)
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
		t.Fatalf("expected cache name to be listed, got %v", core.Join(",", names...))
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
		t.Fatalf("expected cache name removed, got %v", core.Join(",", names...))
	}
}

func TestCache_HTTPCacheStorage_Good_LongURLUsesFixedWidthStorageKey(t *testing.T) {
	medium := coreio.NewMockMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-long-url")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("long-url")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/" + repeatString("a", 4000),
		Method: "GET",
	}
	if err := httpCache.Put(req, cache.CachedResponse{Status: 200, StatusText: "OK"}, []byte("body")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-long-url/long-url/responses/" + key + ".json"
	if _, ok := medium.Files[metaPath]; !ok {
		t.Fatalf("expected fixed-width metadata path %q to exist", metaPath)
	}
	if len(key) != 64 {
		t.Fatalf("expected SHA-256 hex key length 64, got %d", len(key))
	}

	matched, err := httpCache.Match(req)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if matched == nil {
		t.Fatal("expected long URL response to match")
	}
}

func TestCache_HTTPCacheStorage_Keys_Good_EmptyDir(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-empty-keys")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	medium.listErr["/tmp/cache-http-empty-keys"] = fs.ErrNotExist

	names, err := storage.Keys()
	if err != nil {
		t.Fatalf("Keys should treat missing storage dir as empty: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected no cache names, got %v", names)
	}
}

func TestCache_HTTPCacheStorage_Keys_Bad_ListFailure(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-keys-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	medium.listErr["/tmp/cache-http-keys-bad"] = core.E("cache_test", "boom", nil)

	if _, err := storage.Keys(); err == nil {
		t.Fatal("expected Keys to surface backend list failure")
	}
}

func TestCache_HTTPCacheStorage_Delete_Bad_BackendFailure(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-delete-storage-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	medium.deleteAllErr["/tmp/cache-http-delete-storage-bad/blocked"] = core.E("cache_test", "boom", nil)

	if err := storage.Delete("blocked"); err == nil {
		t.Fatal("expected Delete to surface backend failure")
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

func TestCache_HTTPCacheStorage_Close_AllowsReuse(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-close-reuse")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	if err := storage.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	httpCache, err := storage.Open("reused-cache")
	if err != nil {
		t.Fatalf("Open after Close failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/reused",
		Method: "GET",
	}
	resp := cache.CachedResponse{Status: 200, StatusText: "OK"}

	if err := httpCache.Put(req, resp, []byte("ok")); err != nil {
		t.Fatalf("Put after Close failed: %v", err)
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
		t.Fatalf("expected dotted cache name to be listed, got %v", core.Join(",", names...))
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

func TestCache_HTTPCache_Keys_Good_EmptyResponseDir(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-keys-empty")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("keys-empty")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	medium.listErr["/tmp/cache-http-keys-empty/keys-empty/responses"] = fs.ErrNotExist

	urls, err := httpCache.Keys()
	if err != nil {
		t.Fatalf("Keys should treat missing response dir as empty: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("expected no URLs, got %v", urls)
	}
}

func TestCache_HTTPCache_Keys_Bad_ListFailure(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-keys-list-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("keys-list-bad")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	medium.listErr["/tmp/cache-http-keys-list-bad/keys-list-bad/responses"] = core.E("cache_test", "boom", nil)

	if _, err := httpCache.Keys(); err == nil {
		t.Fatal("expected Keys to surface backend list failure")
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

	tests := []struct {
		name string
		resp *cache.CachedResponse
	}{
		{name: "nil", resp: nil},
		{name: "empty", resp: &cache.CachedResponse{}},
		{name: "absolute", resp: &cache.CachedResponse{BodyPath: "/responses/secret.bin"}},
		{name: "traversal", resp: &cache.CachedResponse{BodyPath: "../../etc/passwd"}},
		{name: "wrong-root", resp: &cache.CachedResponse{BodyPath: "config/secret.bin"}},
		{name: "wrong-extension", resp: &cache.CachedResponse{BodyPath: "responses/secret.txt"}},
		{name: "backslash", resp: &cache.CachedResponse{BodyPath: `responses\secret.bin`}},
		{name: "null-byte", resp: &cache.CachedResponse{BodyPath: "responses/secret\x00.bin"}},
		{name: "too-long", resp: &cache.CachedResponse{BodyPath: "responses/" + repeatString("a", 4097) + ".bin"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := httpCache.ReadBody(tt.resp); err == nil {
				t.Fatalf("expected ReadBody to reject %s body path", tt.name)
			}
		})
	}
}

func TestCache_HTTPCacheReadBody_Bad_MissingPayload(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-body-missing")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("body-missing")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/missing",
		Method: "GET",
	}
	resp := cache.CachedResponse{Status: 200, StatusText: "OK"}
	if err := httpCache.Put(req, resp, []byte("body")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	key := httpCacheStorageKey(req)
	bodyPath := "/tmp/cache-http-body-missing/body-missing/responses/" + key + ".bin"
	delete(medium.Files, bodyPath)

	matched, err := httpCache.Match(req)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if matched == nil {
		t.Fatal("expected response metadata to remain")
	}

	if _, err := httpCache.ReadBody(matched); err == nil {
		t.Fatal("expected ReadBody to fail when the body payload is missing")
	}
}

func TestCache_HTTPCache_NilReceiver_Bad(t *testing.T) {
	var httpCache *cache.HTTPCache
	req := cache.CachedRequest{URL: "https://example.com", Method: "GET"}
	resp := cache.CachedResponse{BodyPath: "responses/a.bin"}

	if _, err := httpCache.Match(req); err == nil {
		t.Fatal("expected Match to fail on nil http cache")
	}
	if err := httpCache.Put(req, cache.CachedResponse{}, []byte("body")); err == nil {
		t.Fatal("expected Put to fail on nil http cache")
	}
	if _, err := httpCache.ReadBody(&resp); err == nil {
		t.Fatal("expected ReadBody to fail on nil http cache")
	}
	if err := httpCache.Delete(req); err == nil {
		t.Fatal("expected Delete to fail on nil http cache")
	}
	if _, err := httpCache.Keys(); err == nil {
		t.Fatal("expected Keys to fail on nil http cache")
	}
}

func TestCache_HTTPCache_Delete_Bad_BackendFailure(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-delete-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("delete-bad")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-delete-bad/delete-bad/responses/" + key + ".json"
	medium.deleteErr[metaPath] = core.E("cache_test", "boom", nil)

	if err := httpCache.Delete(req); err == nil {
		t.Fatal("expected Delete to surface backend failure")
	}
}

func TestCache_HTTPCache_Match_Bad_IncompleteEnvelope(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-match-incomplete")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-incomplete")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := legacyHTTPCacheStorageKey(req)
	metaPath := "/tmp/cache-http-match-incomplete/match-incomplete/responses/" + key + ".json"
	medium.Files[metaPath] = `{"request":{"url":"https://example.com/style.css","method":"GET"}}`

	if matched, err := httpCache.Match(req); err == nil || matched != nil {
		t.Fatalf("expected Match to reject incomplete cached response envelope, matched=%v err=%v", matched, err)
	}
}

func TestCache_HTTPCache_Match_Bad_EmptyRequest(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-match-empty")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-empty")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if matched, err := httpCache.Match(cache.CachedRequest{}); err == nil || matched != nil {
		t.Fatalf("expected Match to reject empty request metadata, matched=%v err=%v", matched, err)
	}
}

func TestCache_HTTPCache_LegacyMetadata_Good(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-legacy")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("legacy")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := legacyHTTPCacheStorageKey(req)
	metaPath := "/tmp/cache-http-legacy/legacy/responses/" + key + ".json"
	binPath := "/tmp/cache-http-legacy/legacy/responses/" + key + ".bin"

	legacy := cache.CachedResponse{
		Status:     200,
		StatusText: "OK",
		Headers:    map[string]string{"Content-Type": "text/css"},
		BodyPath:   "responses/" + key + ".bin",
		CachedAt:   time.Now(),
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	medium.Files[metaPath] = string(raw)
	medium.Files[binPath] = "body"

	matched, err := httpCache.Match(req)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if matched == nil {
		t.Fatal("expected legacy cached response to match")
	}
	if matched.Status != 200 || matched.StatusText != "OK" {
		t.Fatalf("unexpected legacy response metadata: %+v", matched)
	}

	body, err := httpCache.ReadBody(matched)
	if err != nil {
		t.Fatalf("ReadBody failed: %v", err)
	}
	if string(body) != "body" {
		t.Fatalf("unexpected legacy body: %q", body)
	}
}

func TestCache_HTTPCache_Put_Bad(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-put-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("put-bad")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := httpCache.Put(cache.CachedRequest{}, cache.CachedResponse{}, []byte("body")); err == nil {
		t.Fatal("expected Put to reject empty request key")
	}
}

func TestCache_HTTPCache_Put_Bad_RequestMetadata(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-put-request-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("put-request-bad")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	tests := []struct {
		name string
		req  cache.CachedRequest
	}{
		{
			name: "invalid-method",
			req: cache.CachedRequest{
				URL:    "https://example.com/style.css",
				Method: "G ET",
			},
		},
		{
			name: "url-control-bytes",
			req: cache.CachedRequest{
				URL:    "https://example.com/\r\nX-Injected: yes",
				Method: "GET",
			},
		},
		{
			name: "method-control-bytes",
			req: cache.CachedRequest{
				URL:    "https://example.com/style.css",
				Method: "GET\r\nX-Injected: yes",
			},
		},
		{
			name: "url-too-long",
			req: cache.CachedRequest{
				URL:    "https://example.com/" + repeatString("a", 8193),
				Method: "GET",
			},
		},
		{
			name: "method-too-long",
			req: cache.CachedRequest{
				URL:    "https://example.com/style.css",
				Method: repeatString("G", 33),
			},
		},
	}

	resp := cache.CachedResponse{Status: 200, StatusText: "OK"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := httpCache.Put(tt.req, resp, []byte("body")); err == nil {
				t.Fatalf("expected Put to reject %s request metadata", tt.name)
			}
		})
	}
}

func TestCache_HTTPCache_Put_Bad_HTTPMetadata(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-put-metadata-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("put-metadata-bad")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}

	tests := []struct {
		name string
		resp cache.CachedResponse
	}{
		{
			name: "status",
			resp: cache.CachedResponse{Status: 0, StatusText: "OK"},
		},
		{
			name: "header-name",
			resp: cache.CachedResponse{
				Status:     200,
				StatusText: "OK",
				Headers:    map[string]string{"X-Inject\r\ned": "value"},
			},
		},
		{
			name: "empty-header-name",
			resp: cache.CachedResponse{
				Status:     200,
				StatusText: "OK",
				Headers:    map[string]string{"": "value"},
			},
		},
		{
			name: "header-value",
			resp: cache.CachedResponse{
				Status:     200,
				StatusText: "OK",
				Headers:    map[string]string{"Content-Type": "text/plain\r\nX-Injected: yes"},
			},
		},
		{
			name: "status-text",
			resp: cache.CachedResponse{Status: 200, StatusText: "OK\r\nInjected"},
		},
		{
			name: "status-text-too-long",
			resp: cache.CachedResponse{Status: 200, StatusText: repeatString("O", 1025)},
		},
		{
			name: "header-name-too-long",
			resp: cache.CachedResponse{
				Status:     200,
				StatusText: "OK",
				Headers:    map[string]string{repeatString("X", 257): "value"},
			},
		},
		{
			name: "header-value-too-long",
			resp: cache.CachedResponse{
				Status:     200,
				StatusText: "OK",
				Headers:    map[string]string{"Content-Type": repeatString("a", 8193)},
			},
		},
		{
			name: "too-many-headers",
			resp: func() cache.CachedResponse {
				headers := make(map[string]string, 129)
				for i := 0; i < 129; i++ {
					headers[core.Concat("X-Test-", string(rune('a'+(i%26))), "-", string(rune('0'+((i/26)%10))))] = "value"
				}
				return cache.CachedResponse{
					Status:     200,
					StatusText: "OK",
					Headers:    headers,
				}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := httpCache.Put(req, tt.resp, []byte("body")); err == nil {
				t.Fatalf("expected Put to reject %s metadata", tt.name)
			}
		})
	}
}

func TestCache_HTTPCache_Put_Ugly(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-put-ugly")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("put-ugly")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{URL: "https://example.com/style.css", Method: "GET"}
	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-put-ugly/put-ugly/responses/" + key + ".json"
	binPath := "/tmp/cache-http-put-ugly/put-ugly/responses/" + key + ".bin"
	medium.writeErr[metaPath] = core.E("cache_test", "metadata boom", nil)

	if err := httpCache.Put(req, cache.CachedResponse{}, []byte("body")); err == nil {
		t.Fatal("expected Put to surface metadata write failure")
	}
	if _, ok := medium.Files[binPath]; ok {
		t.Fatal("expected response body to be cleaned up after metadata write failure")
	}
}

func TestCache_HTTPCache_Match_Bad_RequestMismatch(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-match-mismatch")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-mismatch")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-match-mismatch/match-mismatch/responses/" + key + ".json"

	record := struct {
		Request  cache.CachedRequest  `json:"request"`
		Response cache.CachedResponse `json:"response"`
	}{
		Request: cache.CachedRequest{
			URL:    "https://example.com/wrong.css",
			Method: "GET",
		},
		Response: cache.CachedResponse{
			Status:     200,
			StatusText: "OK",
			Headers:    map[string]string{"Content-Type": "text/css"},
			BodyPath:   "responses/" + key + ".bin",
		},
	}

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	medium.Files[metaPath] = string(raw)

	if matched, err := httpCache.Match(req); err == nil || matched != nil {
		t.Fatalf("expected Match to reject mismatched request metadata, matched=%v err=%v", matched, err)
	}
}

func TestCache_HTTPCache_Match_Bad_BodyPath(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-match-body-path")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-body-path")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-match-body-path/match-body-path/responses/" + key + ".json"

	record := struct {
		Request  cache.CachedRequest  `json:"request"`
		Response cache.CachedResponse `json:"response"`
	}{
		Request: req,
		Response: cache.CachedResponse{
			Status:     200,
			StatusText: "OK",
			Headers:    map[string]string{"Content-Type": "text/css"},
			BodyPath:   "config/secret.bin",
		},
	}

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	medium.Files[metaPath] = string(raw)

	if matched, err := httpCache.Match(req); err == nil || matched != nil {
		t.Fatalf("expected Match to reject invalid body path, matched=%v err=%v", matched, err)
	}
}

func TestCache_HTTPCache_Match_Bad_BodyPathMismatch(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-match-body-path-mismatch")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-body-path-mismatch")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-match-body-path-mismatch/match-body-path-mismatch/responses/" + key + ".json"

	record := struct {
		Request  cache.CachedRequest  `json:"request"`
		Response cache.CachedResponse `json:"response"`
	}{
		Request: req,
		Response: cache.CachedResponse{
			Status:     200,
			StatusText: "OK",
			Headers:    map[string]string{"Content-Type": "text/css"},
			BodyPath:   "responses/other.bin",
		},
	}

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	medium.Files[metaPath] = string(raw)

	if matched, err := httpCache.Match(req); err == nil || matched != nil {
		t.Fatalf("expected Match to reject mismatched body path, matched=%v err=%v", matched, err)
	}
}

func TestCache_HTTPCache_Match_RejectsTamperedMetadata(t *testing.T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-http-match-tampered")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-tampered")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/style.css",
		Method: "GET",
	}
	key := httpCacheStorageKey(req)
	metaPath := "/tmp/cache-http-match-tampered/match-tampered/responses/" + key + ".json"

	record := struct {
		Request  cache.CachedRequest  `json:"request"`
		Response cache.CachedResponse `json:"response"`
	}{
		Request: req,
		Response: cache.CachedResponse{
			Status:     200,
			StatusText: "OK",
			Headers:    map[string]string{"X-Inject\r\ned": "value"},
			BodyPath:   "responses/" + key + ".bin",
		},
	}

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	medium.Files[metaPath] = string(raw)

	if matched, err := httpCache.Match(req); err == nil || matched != nil {
		t.Fatalf("expected Match to reject tampered metadata, matched=%v err=%v", matched, err)
	}
}

func TestCache_HTTPCache_Keys_Good(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-keys")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("keys")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	body := []byte("body")
	if err := httpCache.Put(cache.CachedRequest{URL: "https://example.com/a", Method: "GET"}, cache.CachedResponse{Status: 200, StatusText: "OK"}, body); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if err := httpCache.Put(cache.CachedRequest{URL: "https://example.com/a", Method: "HEAD"}, cache.CachedResponse{Status: 200, StatusText: "OK"}, body); err != nil {
		t.Fatalf("Put duplicate URL failed: %v", err)
	}
	if err := httpCache.Put(cache.CachedRequest{URL: "https://example.com/b", Method: "GET"}, cache.CachedResponse{Status: 200, StatusText: "OK"}, body); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	urls, err := httpCache.Keys()
	if err != nil {
		t.Fatalf("Keys failed: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected deduped URLs, got %v", urls)
	}
	if urls[0] != "https://example.com/a" || urls[1] != "https://example.com/b" {
		t.Fatalf("unexpected sorted URLs: %v", urls)
	}
}

func TestCache_HTTPCache_Match_Bad(t *testing.T) {
	storage, err := cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/cache-http-match-bad")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("match-bad")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	matched, err := httpCache.Match(cache.CachedRequest{URL: "https://example.com/missing", Method: "GET"})
	if err != nil {
		t.Fatalf("Match returned unexpected error: %v", err)
	}
	if matched != nil {
		t.Fatal("expected missing cached response to return nil")
	}
}

func TestCache_ThreatUntrustedKeyDoS_RejectsOversizedKeysOnWritePaths(t *testing.T) {
	c, medium := newTestCache(t, "/tmp/cache-threat-untrusted-key", time.Minute)
	key := repeatString("a", 4097)

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "set",
			fn: func() error {
				return c.Set(key, "value")
			},
		},
		{
			name: "set-with-ttl",
			fn: func() error {
				return c.SetWithTTL(key, "value", time.Minute)
			},
		},
		{
			name: "set-binary",
			fn: func() error {
				return c.SetBinary(key, []byte("value"), "text/plain")
			},
		},
		{
			name: "set-binary-with-ttl",
			fn: func() error {
				return c.SetBinaryWithTTL(key, []byte("value"), "text/plain", time.Minute)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err == nil {
				t.Fatalf("expected %s to reject oversized cache key", tt.name)
			}
		})
	}

	if len(medium.Files) != 0 {
		t.Fatalf("oversized rejected keys should not write cache files, got %d", len(medium.Files))
	}
}

func TestCache_ThreatPathTraversal_ScopedOriginIsHashedAndKeysStillValidated(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-scoped-path", time.Minute)
	scoped := c.Scoped("../../evil\norigin")
	if scoped == nil {
		t.Fatal("expected scoped cache")
	}

	if err := scoped.Set("safe-key", "value"); err != nil {
		t.Fatalf("scoped Set with hostile origin failed: %v", err)
	}
	path, err := scoped.Path("safe-key")
	if err != nil {
		t.Fatalf("scoped Path failed: %v", err)
	}
	if core.Contains(path, "evil") || core.Contains(path, "..") || core.Contains(path, "\n") {
		t.Fatalf("expected scoped path to omit raw origin, got %q", path)
	}

	if err := scoped.Set("../../escape", "value"); err == nil {
		t.Fatal("expected scoped Set to reject traversal key")
	}
}

func TestCache_ThreatPathTraversal_HTTPCacheUsesHashedRequestStorageKeys(t *testing.T) {
	medium := coreio.NewMockMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/cache-threat-http-path")
	if err != nil {
		t.Fatalf("NewCacheStorage failed: %v", err)
	}

	httpCache, err := storage.Open("assets")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := cache.CachedRequest{
		URL:    "https://example.com/../../secret.css?file=../secret",
		Method: "GET",
	}
	resp := cache.CachedResponse{Status: 200, StatusText: "OK"}
	if err := httpCache.Put(req, resp, []byte("body")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	key := httpCacheStorageKey(req)
	if _, ok := medium.Files["/tmp/cache-threat-http-path/assets/responses/"+key+".json"]; !ok {
		t.Fatal("expected HTTP metadata to be stored under hashed request key")
	}
	if _, ok := medium.Files["/tmp/cache-threat-http-path/assets/responses/"+key+".bin"]; !ok {
		t.Fatal("expected HTTP body to be stored under hashed request key")
	}
	for path := range medium.Files {
		if core.Contains(path, "..") || core.Contains(path, "secret.css") {
			t.Fatalf("expected stored path to omit raw request URL, got %q", path)
		}
	}

	matched, err := httpCache.Match(req)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if matched == nil {
		t.Fatal("expected cached response to match")
	}
	if _, err := httpCache.ReadBody(&cache.CachedResponse{BodyPath: "../../escape"}); err == nil {
		t.Fatal("expected ReadBody to reject traversal body path")
	}
}

func TestCache_ThreatTOCTOU_InvalidateOnInvalidateRegistrationIsSnapshotRaceClean(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-invalidate-snapshot", time.Minute)

	if err := c.Set("victim", "old"); err != nil {
		t.Fatalf("Set victim failed: %v", err)
	}
	if err := c.Set("late", "new"); err != nil {
		t.Fatalf("Set late failed: %v", err)
	}

	var registerOnce sync.Once
	var lateCalls int64
	c.OnInvalidate("reload", func(trigger string) []string {
		registerOnce.Do(func() {
			c.OnInvalidate(trigger, func(string) []string {
				atomic.AddInt64(&lateCalls, 1)
				return []string{"late"}
			})
		})
		runtime.Gosched()
		return []string{"victim"}
	})

	deleted, err := c.Invalidate("reload")
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected first invalidation to delete snapshot callback match only, got %d", deleted)
	}
	if got := atomic.LoadInt64(&lateCalls); got != 0 {
		t.Fatalf("newly registered callback should not run in same invalidation pass, got %d calls", got)
	}

	deleted, err = c.Invalidate("reload")
	if err != nil {
		t.Fatalf("second Invalidate failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected second invalidation to delete late callback match, got %d", deleted)
	}
	if got := atomic.LoadInt64(&lateCalls); got != 1 {
		t.Fatalf("expected late callback to run once on next invalidation pass, got %d calls", got)
	}
}

func TestCache_ThreatTOCTOU_InvalidateConcurrentRegistrationRaceClean(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-invalidate-race", time.Minute)
	c.OnInvalidate("reload", func(string) []string {
		runtime.Gosched()
		return nil
	})

	const workers = 16
	const registrationsPerWorker = 16
	start := make(chan struct{})
	errCh := make(chan error, workers)

	var done sync.WaitGroup
	done.Add(workers * 2)
	for range workers {
		go func() {
			defer done.Done()
			<-start
			for range registrationsPerWorker {
				c.OnInvalidate("reload", func(string) []string {
					runtime.Gosched()
					return nil
				})
			}
		}()
	}
	for range workers {
		go func() {
			defer done.Done()
			<-start
			for range registrationsPerWorker {
				if _, err := c.Invalidate("reload"); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	close(start)
	done.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Invalidate failed: %v", err)
	}
}

func TestCache_ThreatTOCTOU_ExpiredGetConcurrentReadersReturnNotFound(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-expired-get", time.Minute)
	if err := c.SetWithTTL("ttl/race", map[string]string{"state": "expired"}, time.Nanosecond); err != nil {
		t.Fatalf("SetWithTTL failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	const readers = 64
	start := make(chan struct{})
	errCh := make(chan string, readers)

	var done sync.WaitGroup
	done.Add(readers)
	for range readers {
		go func() {
			defer done.Done()
			<-start

			got := map[string]string{"state": "sentinel"}
			found, err := c.Get("ttl/race", &got)
			if err != nil {
				errCh <- err.Error()
				return
			}
			if found {
				errCh <- "expected expired Get to return found=false"
				return
			}
			if got["state"] != "sentinel" {
				errCh <- "expired Get unmarshaled stale data into destination"
			}
		}()
	}

	close(start)
	done.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}
}

func TestCache_ThreatTOCTOU_ConcurrentSetRandomKeysRaceClean(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-concurrent-random-set", time.Minute)

	const workers = 100
	keys := make([]string, workers)
	for i := range workers {
		keys[i] = "race/random/" + core.Itoa((i*37+11)%workers)
	}

	start := make(chan struct{})
	errCh := make(chan string, workers)

	var done sync.WaitGroup
	done.Add(workers)
	for i, key := range keys {
		go func(value int, key string) {
			defer done.Done()
			<-start
			if err := c.Set(key, map[string]int{"writer": value}); err != nil {
				errCh <- err.Error()
			}
		}(i, key)
	}

	close(start)
	done.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}

	foundCount := 0
	for i, key := range keys {
		var got map[string]int
		found, err := c.Get(key, &got)
		if err != nil {
			t.Fatalf("Get %q failed: %v", key, err)
		}
		if !found {
			continue
		}
		foundCount++
		if got["writer"] != i {
			t.Fatalf("expected %q writer %d, got %d", key, i, got["writer"])
		}
	}
	if foundCount != workers {
		t.Fatalf("expected %d entries after concurrent Set calls, got %d", workers, foundCount)
	}
}

func TestCache_ThreatTOCTOU_ConcurrentSetSameKeyRaceClean(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-concurrent-same-set", time.Minute)

	const workers = 100
	written := make(map[int]struct{}, workers)
	for i := range workers {
		written[i] = struct{}{}
	}

	start := make(chan struct{})
	errCh := make(chan string, workers)

	var done sync.WaitGroup
	done.Add(workers)
	for i := range workers {
		go func(value int) {
			defer done.Done()
			<-start
			if err := c.Set("race/same", map[string]int{"writer": value}); err != nil {
				errCh <- err.Error()
			}
		}(i)
	}

	close(start)
	done.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}

	var got map[string]int
	found, err := c.Get("race/same", &got)
	if err != nil {
		t.Fatalf("final Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected final cache entry to exist")
	}
	if _, ok := written[got["writer"]]; !ok {
		t.Fatalf("final writer %d was not one of the concurrent writers", got["writer"])
	}
}

func TestCache_ThreatTOCTOU_ConcurrentGetSetDeleteSameKeyRaceClean(t *testing.T) {
	c, _ := newTestCache(t, "/tmp/cache-threat-concurrent-mixed", time.Minute)
	if err := c.Set("race/mixed", map[string]int{"writer": -1}); err != nil {
		t.Fatalf("initial Set failed: %v", err)
	}

	const workers = 100
	const operations = 10
	start := make(chan struct{})
	errCh := make(chan string, workers*operations)

	var done sync.WaitGroup
	done.Add(workers)
	for i := range workers {
		go func(value int) {
			defer done.Done()
			<-start
			for op := range operations {
				switch (value + op) % 3 {
				case 0:
					var got map[string]int
					found, err := c.Get("race/mixed", &got)
					if err != nil {
						errCh <- err.Error()
						continue
					}
					if found && (got["writer"] < -1 || got["writer"] >= workers) {
						errCh <- "Get returned a writer outside the written range"
					}
				case 1:
					if err := c.Set("race/mixed", map[string]int{"writer": value}); err != nil {
						errCh <- err.Error()
					}
				default:
					if err := c.Delete("race/mixed"); err != nil {
						errCh <- err.Error()
					}
				}
			}
		}(i)
	}

	close(start)
	done.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}
}

func TestCache_ThreatTOCTOU_GetThenSetSerializesEntryWrites(t *testing.T) {
	medium := &raceProbeMedium{MockMedium: coreio.NewMockMedium()}
	c, err := cache.New(medium, "/tmp/cache-threat-toctou", time.Minute)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	const workers = 32
	start := make(chan struct{})
	writes := make(chan struct{})
	errCh := make(chan string, workers*3)

	var reads sync.WaitGroup
	reads.Add(workers)
	var done sync.WaitGroup
	done.Add(workers)

	for i := range workers {
		go func(value int) {
			defer done.Done()
			<-start

			var got map[string]int
			found, err := c.Get("race/key", &got)
			if err != nil {
				errCh <- err.Error()
			}
			if found {
				errCh <- "expected initial Get to miss"
			}
			reads.Done()

			<-writes
			if err := c.Set("race/key", map[string]int{"writer": value}); err != nil {
				errCh <- err.Error()
			}
		}(i)
	}

	close(start)
	reads.Wait()
	close(writes)
	done.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}

	var got map[string]int
	found, err := c.Get("race/key", &got)
	if err != nil {
		t.Fatalf("final Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected final cache entry to exist")
	}
	if medium.probedWrites == 0 {
		t.Fatal("expected probe medium to observe writes")
	}
}

type raceProbeMedium struct {
	*coreio.MockMedium
	probedWrites int
}

func (m *raceProbeMedium) Write(path, content string) error {
	m.probedWrites++
	runtime.Gosched()
	m.probedWrites++
	return m.MockMedium.Write(path, content)
}
