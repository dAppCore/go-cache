// SPDX-License-Identifier: EUPL-1.2

package cache_test

import (
	"io/fs"
	"time"

	. "dappco.re/go"
	"dappco.re/go/cache"
	coreio "dappco.re/go/io"
)

const (
	testBase             = "/tmp/go-cache-v090"
	testKey              = "agent/profile"
	testTextPlain        = "text/plain"
	testAppOrigin        = "https://app.example"
	testAdminOrigin      = "https://admin.example"
	testURL              = "https://example.com/data"
	testHeaderName       = "Content-Type"
	testHeaderValue      = "text/plain"
	testLongCacheKeySize = 4097
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

func testCache(t *T, baseDir string) (*cache.Cache, *coreio.MockMedium) {
	t.Helper()
	medium := coreio.NewMockMedium()
	r := cache.New(medium, baseDir, time.Minute)
	RequireTrue(t, r.OK)
	return r.Value.(*cache.Cache), medium
}

func testStorage(t *T, baseDir string) (*cache.CacheStorage, *coreio.MockMedium) {
	t.Helper()
	medium := coreio.NewMockMedium()
	r := cache.NewCacheStorage(medium, baseDir)
	RequireTrue(t, r.OK)
	return r.Value.(*cache.CacheStorage), medium
}

func testHTTPCache(t *T, baseDir, name string) (*cache.HTTPCache, *coreio.MockMedium) {
	t.Helper()
	storage, medium := testStorage(t, baseDir)
	r := storage.Open(name)
	RequireTrue(t, r.OK)
	return r.Value.(*cache.HTTPCache), medium
}

func longString(s string, count int) string {
	builder := NewBuilder()
	for range count {
		builder.WriteString(s)
	}
	return builder.String()
}

func request(method, url string) cache.CachedRequest {
	return cache.CachedRequest{Method: method, URL: url}
}

func response(status int) cache.CachedResponse {
	return cache.CachedResponse{
		Status:     status,
		StatusText: "OK",
		Headers:    map[string]string{testHeaderName: testHeaderValue},
	}
}

func resultString(t *T, r Result) string {
	t.Helper()
	RequireTrue(t, r.OK)
	return r.Value.(string)
}

func resultBool(t *T, r Result) bool {
	t.Helper()
	RequireTrue(t, r.OK)
	return r.Value.(bool)
}

func resultInt(t *T, r Result) int {
	t.Helper()
	RequireTrue(t, r.OK)
	return r.Value.(int)
}

func resultBytes(t *T, r Result) []byte {
	t.Helper()
	RequireTrue(t, r.OK)
	return r.Value.([]byte)
}

func TestCache_New_Good(t *T) {
	r := cache.New(coreio.NewMockMedium(), testBase+"/new-good", time.Minute)
	RequireTrue(t, r.OK)
	c := r.Value.(*cache.Cache)
	AssertTrue(t, c.Set(testKey, map[string]string{"name": "codex"}).OK)
}

func TestCache_New_Bad(t *T) {
	r := cache.New(coreio.NewMockMedium(), testBase+"/new-bad", -time.Second)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "ttl")
}

func TestCache_New_Ugly(t *T) {
	r := cache.New(nil, t.TempDir(), 0)
	RequireTrue(t, r.OK)
	c := r.Value.(*cache.Cache)
	AssertTrue(t, c.Delete("missing/key").OK)
}

func TestCache_Path_Good(t *T) {
	c, _ := testCache(t, testBase+"/path-good")
	path := resultString(t, c.Path(testKey))
	AssertContains(t, path, testKey+".json")
	AssertContains(t, path, "path-good")
}

func TestCache_Path_Bad(t *T) {
	c, _ := testCache(t, testBase+"/path-bad")
	r := c.Path("../escape")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "traversal")
}

func TestCache_Path_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/path-ugly")
	r := c.Path(longString("a", testLongCacheKeySize))
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "too long")
}

func TestCache_Get_Good(t *T) {
	c, _ := testCache(t, testBase+"/get-good")
	RequireTrue(t, c.Set(testKey, map[string]string{"name": "codex"}).OK)
	var got map[string]string
	AssertTrue(t, resultBool(t, c.Get(testKey, &got)))
	AssertEqual(t, "codex", got["name"])
}

func TestCache_Get_Bad(t *T) {
	c, _ := testCache(t, testBase+"/get-bad")
	var got map[string]string
	found := resultBool(t, c.Get("missing/key", &got))
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_Get_Ugly(t *T) {
	c, medium := testCache(t, testBase+"/get-ugly")
	path := resultString(t, c.Path(testKey))
	RequireNoError(t, medium.Write(path, "{not-json"))
	var got map[string]string
	r := c.Get(testKey, &got)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "unmarshal")
}

func TestCache_Set_Good(t *T) {
	c, medium := testCache(t, testBase+"/set-good")
	r := c.Set(testKey, map[string]string{"mode": "good"})
	RequireTrue(t, r.OK)
	AssertTrue(t, medium.IsFile(resultString(t, c.Path(testKey))))
}

func TestCache_Set_Bad(t *T) {
	c, _ := testCache(t, testBase+"/set-bad")
	r := c.Set(testKey, make(chan int))
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "marshal")
}

func TestCache_Set_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/set-ugly")
	r := c.Set("edge/empty", map[string]string{})
	RequireTrue(t, r.OK)
	var got map[string]string
	AssertTrue(t, resultBool(t, c.Get("edge/empty", &got)))
}

func TestCache_SetWithTTL_Good(t *T) {
	c, _ := testCache(t, testBase+"/ttl-good")
	r := c.SetWithTTL(testKey, "fresh", time.Minute)
	RequireTrue(t, r.OK)
	var got string
	AssertTrue(t, resultBool(t, c.Get(testKey, &got)))
}

func TestCache_SetWithTTL_Bad(t *T) {
	c, _ := testCache(t, testBase+"/ttl-bad")
	r := c.SetWithTTL(testKey, "expired", -time.Second)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "ttl")
}

func TestCache_SetWithTTL_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/ttl-ugly")
	RequireTrue(t, c.SetWithTTL(testKey, "now", 0).OK)
	var got string
	AssertFalse(t, resultBool(t, c.Get(testKey, &got)))
}

func TestCache_Delete_Good(t *T) {
	c, medium := testCache(t, testBase+"/delete-good")
	RequireTrue(t, c.Set(testKey, "delete").OK)
	RequireTrue(t, c.Delete(testKey).OK)
	AssertFalse(t, medium.IsFile(resultString(t, c.Path(testKey))))
}

func TestCache_Delete_Bad(t *T) {
	var c *cache.Cache
	r := c.Delete(testKey)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_Delete_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/delete-ugly")
	r := c.Delete("missing/key")
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_SetBinary_Good(t *T) {
	c, _ := testCache(t, testBase+"/binary-good")
	RequireTrue(t, c.SetBinary("artifact/blob", []byte("wasm"), "application/wasm").OK)
	AssertEqual(t, "wasm", string(resultBytes(t, c.GetBinary("artifact/blob"))))
}

func TestCache_SetBinary_Bad(t *T) {
	var c *cache.Cache
	r := c.SetBinary("artifact/blob", []byte("x"), testTextPlain)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_SetBinary_Ugly(t *T) {
	medium := newScriptedMedium()
	c := cache.New(medium, testBase+"/binary-ugly", time.Minute).Value.(*cache.Cache)
	path := resultString(t, c.Path("artifact/blob"))
	medium.writeErr[TrimSuffix(path, ".json")+".bin"] = AnError
	r := c.SetBinary("artifact/blob", []byte("x"), testTextPlain)
	AssertFalse(t, r.OK)
	AssertFalse(t, medium.IsFile(path))
}

func TestCache_SetBinaryWithTTL_Good(t *T) {
	c, _ := testCache(t, testBase+"/binary-ttl-good")
	r := c.SetBinaryWithTTL("artifact/blob", []byte("abc"), testTextPlain, time.Minute)
	RequireTrue(t, r.OK)
	AssertEqual(t, "abc", string(resultBytes(t, c.GetBinary("artifact/blob"))))
}

func TestCache_SetBinaryWithTTL_Bad(t *T) {
	c, _ := testCache(t, testBase+"/binary-ttl-bad")
	r := c.SetBinaryWithTTL("artifact/blob", []byte("abc"), testTextPlain, -time.Second)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "ttl")
}

func TestCache_SetBinaryWithTTL_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/binary-ttl-ugly")
	RequireTrue(t, c.SetBinaryWithTTL("artifact/blob", []byte("abc"), testTextPlain, 0).OK)
	AssertNil(t, c.GetBinary("artifact/blob").Value)
}

func TestCache_GetBinary_Good(t *T) {
	c, _ := testCache(t, testBase+"/get-binary-good")
	RequireTrue(t, c.SetBinary("artifact/blob", []byte{0, 1, 2}, testTextPlain).OK)
	AssertEqual(t, []byte{0, 1, 2}, resultBytes(t, c.GetBinary("artifact/blob")))
}

func TestCache_GetBinary_Bad(t *T) {
	c, _ := testCache(t, testBase+"/get-binary-bad")
	r := c.GetBinary("missing/blob")
	RequireTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_GetBinary_Ugly(t *T) {
	c, medium := testCache(t, testBase+"/get-binary-ugly")
	path := resultString(t, c.Path("artifact/blob"))
	RequireNoError(t, medium.Write(path, "{bad-json"))
	r := c.GetBinary("artifact/blob")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "metadata")
}

func TestCache_DeleteMany_Good(t *T) {
	c, medium := testCache(t, testBase+"/delete-many-good")
	RequireTrue(t, c.Set("agent/one", "1").OK)
	RequireTrue(t, c.Set("agent/two", "2").OK)
	RequireTrue(t, c.DeleteMany("agent/one", "agent/two").OK)
	AssertFalse(t, medium.IsFile(resultString(t, c.Path("agent/one"))))
}

func TestCache_DeleteMany_Bad(t *T) {
	c, medium := testCache(t, testBase+"/delete-many-bad")
	RequireTrue(t, c.Set("agent/keep", "1").OK)
	r := c.DeleteMany("../escape", "agent/keep")
	AssertFalse(t, r.OK)
	AssertTrue(t, medium.IsFile(resultString(t, c.Path("agent/keep"))))
}

func TestCache_DeleteMany_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/delete-many-ugly")
	r := c.DeleteMany("missing/one", "missing/two")
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_OnInvalidate_Good(t *T) {
	c, _ := testCache(t, testBase+"/on-invalidate-good")
	c.OnInvalidate("profile.changed", func(string) []string { return []string{"agent/*"} })
	RequireTrue(t, c.Set(testKey, "value").OK)
	AssertEqual(t, 1, resultInt(t, c.Invalidate("profile.changed")))
}

func TestCache_OnInvalidate_Bad(t *T) {
	c, _ := testCache(t, testBase+"/on-invalidate-bad")
	c.OnInvalidate("profile.changed", nil)
	RequireTrue(t, c.Set(testKey, "value").OK)
	AssertEqual(t, 0, resultInt(t, c.Invalidate("profile.changed")))
}

func TestCache_OnInvalidate_Ugly(t *T) {
	var c *cache.Cache
	c.OnInvalidate("profile.changed", func(string) []string { return []string{"agent/*"} })
	AssertNil(t, c)
}

func TestCache_Invalidate_Good(t *T) {
	c, _ := testCache(t, testBase+"/invalidate-good")
	c.OnInvalidate("profile.changed", func(string) []string { return []string{"agent/*"} })
	RequireTrue(t, c.Set(testKey, "value").OK)
	AssertEqual(t, 1, resultInt(t, c.Invalidate("profile.changed")))
}

func TestCache_Invalidate_Bad(t *T) {
	c, _ := testCache(t, testBase+"/invalidate-bad")
	c.OnInvalidate("bad", func(string) []string { return []string{longString("a", testLongCacheKeySize)} })
	r := c.Invalidate("bad")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "pattern")
}

func TestCache_Invalidate_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/invalidate-ugly")
	deleted := resultInt(t, c.Invalidate("no.callbacks"))
	AssertEqual(t, 0, deleted)
}

func TestCache_Scoped_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped != nil)
	AssertTrue(t, scoped.Set("prefs/theme", "dark").OK)
}

func TestCache_Scoped_Bad(t *T) {
	var c *cache.Cache
	scoped := c.Scoped(testAppOrigin)
	AssertNil(t, scoped)
	AssertEqual(t, (*cache.ScopedCache)(nil), scoped)
}

func TestCache_Scoped_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-ugly")
	scoped := c.Scoped("")
	RequireTrue(t, scoped != nil)
	AssertTrue(t, scoped.Set("prefs/theme", "dark").OK)
}

func TestCache_ClearScope_Good(t *T) {
	c, _ := testCache(t, testBase+"/clear-scope-good")
	RequireTrue(t, c.Scoped(testAppOrigin).Set("prefs/theme", "dark").OK)
	RequireTrue(t, c.ClearScope(testAppOrigin).OK)
	AssertEqual(t, time.Duration(-1), c.Scoped(testAppOrigin).Age("prefs/theme"))
}

func TestCache_ClearScope_Bad(t *T) {
	var c *cache.Cache
	r := c.ClearScope(testAppOrigin)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ClearScope_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/clear-scope-ugly")
	r := c.ClearScope("no-entries")
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_Clear_Good(t *T) {
	c, _ := testCache(t, testBase+"/clear-good")
	RequireTrue(t, c.Set(testKey, "value").OK)
	r := c.Clear()
	AssertTrue(t, r.OK)
	AssertEqual(t, time.Duration(-1), c.Age(testKey))
}

func TestCache_Clear_Bad(t *T) {
	var c *cache.Cache
	r := c.Clear()
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_Clear_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/clear-ugly")
	RequireTrue(t, c.Clear().OK)
	r := c.Clear()
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "clear")
}

func TestCache_Age_Good(t *T) {
	c, _ := testCache(t, testBase+"/age-good")
	RequireTrue(t, c.Set(testKey, "value").OK)
	age := c.Age(testKey)
	AssertTrue(t, age >= 0)
}

func TestCache_Age_Bad(t *T) {
	c, _ := testCache(t, testBase+"/age-bad")
	age := c.Age("missing/key")
	AssertEqual(t, time.Duration(-1), age)
}

func TestCache_Age_Ugly(t *T) {
	var c *cache.Cache
	age := c.Age(testKey)
	AssertEqual(t, time.Duration(-1), age)
}

func TestCache_GitHubReposKey_Good(t *T) {
	key := cache.GitHubReposKey("acme")
	AssertEqual(t, "github/acme/repos", key)
	AssertContains(t, key, "repos")
}

func TestCache_GitHubReposKey_Bad(t *T) {
	key := cache.GitHubReposKey("acme/widgets")
	AssertContains(t, key, "acme%2Fwidgets")
	AssertNotContains(t, key, "acme/widgets")
}

func TestCache_GitHubReposKey_Ugly(t *T) {
	key := cache.GitHubReposKey("")
	AssertEqual(t, "github//repos", key)
	AssertContains(t, key, "github")
}

func TestCache_GitHubRepoKey_Good(t *T) {
	key := cache.GitHubRepoKey("acme", "widgets")
	AssertEqual(t, "github/acme/widgets/meta", key)
	AssertContains(t, key, "meta")
}

func TestCache_GitHubRepoKey_Bad(t *T) {
	key := cache.GitHubRepoKey("acme/widgets", "api server")
	AssertContains(t, key, "acme%2Fwidgets")
	AssertContains(t, key, "api%20server")
}

func TestCache_GitHubRepoKey_Ugly(t *T) {
	key := cache.GitHubRepoKey("", "")
	AssertEqual(t, "github///meta", key)
	AssertContains(t, key, "github")
}

func TestCache_ScopedCache_Scoped_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-nested-good")
	nested := c.Scoped(testAppOrigin).Scoped(testAdminOrigin)
	RequireTrue(t, nested != nil)
	AssertTrue(t, nested.Set("prefs/theme", "dark").OK)
}

func TestCache_ScopedCache_Scoped_Bad(t *T) {
	var scoped *cache.ScopedCache
	nested := scoped.Scoped(testAdminOrigin)
	AssertNil(t, nested)
	AssertEqual(t, (*cache.ScopedCache)(nil), nested)
}

func TestCache_ScopedCache_Scoped_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-nested-ugly")
	nested := c.Scoped(testAppOrigin).Scoped("")
	RequireTrue(t, nested != nil)
	AssertTrue(t, nested.Set("prefs/theme", "dark").OK)
}

func TestCache_ScopedCache_Path_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-path-good")
	path := resultString(t, c.Scoped(testAppOrigin).Path("prefs/theme"))
	AssertContains(t, path, "scope_")
	AssertContains(t, path, "prefs/theme.json")
}

func TestCache_ScopedCache_Path_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.Path("prefs/theme")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_Path_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-path-ugly")
	r := c.Scoped(testAppOrigin).Path("../escape")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "traversal")
}

func TestCache_ScopedCache_Get_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-get-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.Set("prefs/theme", "dark").OK)
	var got string
	AssertTrue(t, resultBool(t, scoped.Get("prefs/theme", &got)))
}

func TestCache_ScopedCache_Get_Bad(t *T) {
	var scoped *cache.ScopedCache
	var got string
	r := scoped.Get("prefs/theme", &got)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_Get_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-get-ugly")
	var got string
	AssertFalse(t, resultBool(t, c.Scoped(testAppOrigin).Get("missing/key", &got)))
}

func TestCache_ScopedCache_Set_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-set-good")
	scoped := c.Scoped(testAppOrigin)
	AssertTrue(t, scoped.Set("prefs/theme", "dark").OK)
	AssertTrue(t, scoped.Age("prefs/theme") >= 0)
}

func TestCache_ScopedCache_Set_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.Set("prefs/theme", "dark")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_Set_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-set-ugly")
	r := c.Scoped(testAppOrigin).Set("../escape", "dark")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "traversal")
}

func TestCache_ScopedCache_SetWithTTL_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-ttl-good")
	scoped := c.Scoped(testAppOrigin)
	AssertTrue(t, scoped.SetWithTTL("prefs/theme", "dark", time.Minute).OK)
	AssertTrue(t, scoped.Age("prefs/theme") >= 0)
}

func TestCache_ScopedCache_SetWithTTL_Bad(t *T) {
	c, _ := testCache(t, testBase+"/scoped-ttl-bad")
	r := c.Scoped(testAppOrigin).SetWithTTL("prefs/theme", "dark", -time.Second)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "ttl")
}

func TestCache_ScopedCache_SetWithTTL_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-ttl-ugly")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.SetWithTTL("prefs/theme", "dark", 0).OK)
	var got string
	AssertFalse(t, resultBool(t, scoped.Get("prefs/theme", &got)))
}

func TestCache_ScopedCache_SetBinary_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-bin-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.SetBinary("artifact/blob", []byte("abc"), testTextPlain).OK)
	AssertEqual(t, "abc", string(resultBytes(t, scoped.GetBinary("artifact/blob"))))
}

func TestCache_ScopedCache_SetBinary_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.SetBinary("artifact/blob", []byte("abc"), testTextPlain)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_SetBinary_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-bin-ugly")
	r := c.Scoped(testAppOrigin).SetBinary("../escape", []byte("abc"), testTextPlain)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "traversal")
}

func TestCache_ScopedCache_SetBinaryWithTTL_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-bin-ttl-good")
	scoped := c.Scoped(testAppOrigin)
	AssertTrue(t, scoped.SetBinaryWithTTL("artifact/blob", []byte("abc"), testTextPlain, time.Minute).OK)
	AssertEqual(t, "abc", string(resultBytes(t, scoped.GetBinary("artifact/blob"))))
}

func TestCache_ScopedCache_SetBinaryWithTTL_Bad(t *T) {
	c, _ := testCache(t, testBase+"/scoped-bin-ttl-bad")
	r := c.Scoped(testAppOrigin).SetBinaryWithTTL("artifact/blob", []byte("abc"), testTextPlain, -time.Second)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "ttl")
}

func TestCache_ScopedCache_SetBinaryWithTTL_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-bin-ttl-ugly")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.SetBinaryWithTTL("artifact/blob", []byte("abc"), testTextPlain, 0).OK)
	AssertNil(t, scoped.GetBinary("artifact/blob").Value)
}

func TestCache_ScopedCache_GetBinary_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-get-bin-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.SetBinary("artifact/blob", []byte("abc"), testTextPlain).OK)
	AssertEqual(t, []byte("abc"), resultBytes(t, scoped.GetBinary("artifact/blob")))
}

func TestCache_ScopedCache_GetBinary_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.GetBinary("artifact/blob")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_GetBinary_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-get-bin-ugly")
	r := c.Scoped(testAppOrigin).GetBinary("missing/blob")
	RequireTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_ScopedCache_Delete_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-delete-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.Set("prefs/theme", "dark").OK)
	AssertTrue(t, scoped.Delete("prefs/theme").OK)
	AssertEqual(t, time.Duration(-1), scoped.Age("prefs/theme"))
}

func TestCache_ScopedCache_Delete_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.Delete("prefs/theme")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_Delete_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-delete-ugly")
	r := c.Scoped(testAppOrigin).Delete("missing/key")
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_ScopedCache_DeleteMany_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-delete-many-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.Set("prefs/one", "1").OK)
	RequireTrue(t, scoped.Set("prefs/two", "2").OK)
	AssertTrue(t, scoped.DeleteMany("prefs/one", "prefs/two").OK)
}

func TestCache_ScopedCache_DeleteMany_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.DeleteMany("prefs/one")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_DeleteMany_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-delete-many-ugly")
	r := c.Scoped(testAppOrigin).DeleteMany("../escape")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "traversal")
}

func TestCache_ScopedCache_Clear_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-clear-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.Set("prefs/theme", "dark").OK)
	AssertTrue(t, scoped.Clear().OK)
	AssertEqual(t, time.Duration(-1), scoped.Age("prefs/theme"))
}

func TestCache_ScopedCache_Clear_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.Clear()
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_Clear_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-clear-ugly")
	r := c.Scoped(testAppOrigin).Clear()
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_ScopedCache_ClearScope_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-clear-scope-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.Scoped(testAdminOrigin).Set("prefs/theme", "dark").OK)
	AssertTrue(t, scoped.ClearScope(testAdminOrigin).OK)
}

func TestCache_ScopedCache_ClearScope_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.ClearScope(testAdminOrigin)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_ClearScope_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-clear-scope-ugly")
	r := c.Scoped(testAppOrigin).ClearScope("empty")
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_ScopedCache_OnInvalidate_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-on-invalidate-good")
	scoped := c.Scoped(testAppOrigin)
	scoped.OnInvalidate("prefs.changed", func(string) []string { return []string{"prefs/*"} })
	RequireTrue(t, scoped.Set("prefs/theme", "dark").OK)
	AssertEqual(t, 1, resultInt(t, scoped.Invalidate("prefs.changed")))
}

func TestCache_ScopedCache_OnInvalidate_Bad(t *T) {
	c, _ := testCache(t, testBase+"/scoped-on-invalidate-bad")
	scoped := c.Scoped(testAppOrigin)
	scoped.OnInvalidate("prefs.changed", nil)
	AssertEqual(t, 0, resultInt(t, scoped.Invalidate("prefs.changed")))
}

func TestCache_ScopedCache_OnInvalidate_Ugly(t *T) {
	var scoped *cache.ScopedCache
	scoped.OnInvalidate("prefs.changed", func(string) []string { return []string{"prefs/*"} })
	AssertNil(t, scoped)
}

func TestCache_ScopedCache_Invalidate_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-invalidate-good")
	scoped := c.Scoped(testAppOrigin)
	scoped.OnInvalidate("prefs.changed", func(string) []string { return []string{"prefs/*"} })
	RequireTrue(t, scoped.Set("prefs/theme", "dark").OK)
	AssertEqual(t, 1, resultInt(t, scoped.Invalidate("prefs.changed")))
}

func TestCache_ScopedCache_Invalidate_Bad(t *T) {
	var scoped *cache.ScopedCache
	r := scoped.Invalidate("prefs.changed")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_ScopedCache_Invalidate_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-invalidate-ugly")
	deleted := resultInt(t, c.Scoped(testAppOrigin).Invalidate("none"))
	AssertEqual(t, 0, deleted)
}

func TestCache_ScopedCache_Age_Good(t *T) {
	c, _ := testCache(t, testBase+"/scoped-age-good")
	scoped := c.Scoped(testAppOrigin)
	RequireTrue(t, scoped.Set("prefs/theme", "dark").OK)
	AssertTrue(t, scoped.Age("prefs/theme") >= 0)
}

func TestCache_ScopedCache_Age_Bad(t *T) {
	var scoped *cache.ScopedCache
	age := scoped.Age("prefs/theme")
	AssertEqual(t, time.Duration(-1), age)
}

func TestCache_ScopedCache_Age_Ugly(t *T) {
	c, _ := testCache(t, testBase+"/scoped-age-ugly")
	age := c.Scoped(testAppOrigin).Age("missing/key")
	AssertEqual(t, time.Duration(-1), age)
}

func TestCache_NewCacheStorage_Good(t *T) {
	r := cache.NewCacheStorage(coreio.NewMockMedium(), testBase+"/storage-good")
	RequireTrue(t, r.OK)
	storage := r.Value.(*cache.CacheStorage)
	AssertTrue(t, storage.Close().OK)
}

func TestCache_NewCacheStorage_Bad(t *T) {
	medium := newScriptedMedium()
	medium.ensureDirErr[testBase+"/storage-bad"] = AnError
	r := cache.NewCacheStorage(medium, testBase+"/storage-bad")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "create")
}

func TestCache_NewCacheStorage_Ugly(t *T) {
	r := cache.NewCacheStorage(nil, t.TempDir())
	RequireTrue(t, r.OK)
	storage := r.Value.(*cache.CacheStorage)
	AssertTrue(t, storage.Close().OK)
}

func TestCache_CacheStorage_Open_Good(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-open-good")
	r := storage.Open("app-v1")
	RequireTrue(t, r.OK)
	AssertNotNil(t, r.Value.(*cache.HTTPCache))
}

func TestCache_CacheStorage_Open_Bad(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-open-bad")
	r := storage.Open("../escape")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "cache name")
}

func TestCache_CacheStorage_Open_Ugly(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-open-ugly")
	first := storage.Open("app-v1").Value.(*cache.HTTPCache)
	second := storage.Open("app-v1").Value.(*cache.HTTPCache)
	AssertEqual(t, first, second)
}

func TestCache_CacheStorage_Delete_Good(t *T) {
	storage, medium := testStorage(t, testBase+"/storage-delete-good")
	RequireTrue(t, storage.Open("app-v1").OK)
	AssertTrue(t, storage.Delete("app-v1").OK)
	AssertFalse(t, medium.IsFile(testBase+"/storage-delete-good/app-v1"))
}

func TestCache_CacheStorage_Delete_Bad(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-delete-bad")
	r := storage.Delete("../escape")
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "cache name")
}

func TestCache_CacheStorage_Delete_Ugly(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-delete-ugly")
	r := storage.Delete("missing-cache")
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_CacheStorage_Keys_Good(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-keys-good")
	RequireTrue(t, storage.Open("app-v1").OK)
	keys := storage.Keys().Value.([]string)
	AssertEqual(t, []string{"app-v1"}, keys)
}

func TestCache_CacheStorage_Keys_Bad(t *T) {
	medium := newScriptedMedium()
	RequireTrue(t, cache.NewCacheStorage(medium, testBase+"/storage-keys-bad").OK)
	storage := cache.NewCacheStorage(medium, testBase+"/storage-keys-bad").Value.(*cache.CacheStorage)
	medium.listErr[testBase+"/storage-keys-bad"] = AnError
	r := storage.Keys()
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "list")
}

func TestCache_CacheStorage_Keys_Ugly(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-keys-ugly")
	keys := storage.Keys().Value.([]string)
	AssertEqual(t, 0, len(keys))
}

func TestCache_CacheStorage_Close_Good(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-close-good")
	RequireTrue(t, storage.Open("app-v1").OK)
	AssertTrue(t, storage.Close().OK)
}

func TestCache_CacheStorage_Close_Bad(t *T) {
	var storage *cache.CacheStorage
	r := storage.Close()
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_CacheStorage_Close_Ugly(t *T) {
	storage, _ := testStorage(t, testBase+"/storage-close-ugly")
	RequireTrue(t, storage.Close().OK)
	r := storage.Open("app-v1")
	AssertTrue(t, r.OK)
	AssertNotNil(t, r.Value)
}

func TestCache_HTTPCache_Match_Good(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-match-good", "app-v1")
	req := request("GET", testURL)
	RequireTrue(t, httpCache.Put(req, response(200), []byte("ok")).OK)
	match := httpCache.Match(req).Value.(*cache.CachedResponse)
	AssertEqual(t, 200, match.Status)
}

func TestCache_HTTPCache_Match_Bad(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-match-bad", "app-v1")
	r := httpCache.Match(request("", testURL))
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "request")
}

func TestCache_HTTPCache_Match_Ugly(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-match-ugly", "app-v1")
	r := httpCache.Match(request("GET", testURL))
	RequireTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_HTTPCache_Put_Good(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-put-good", "app-v1")
	req := request("GET", testURL)
	r := httpCache.Put(req, response(201), []byte("created"))
	AssertTrue(t, r.OK)
	AssertNotNil(t, httpCache.Match(req).Value)
}

func TestCache_HTTPCache_Put_Bad(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-put-bad", "app-v1")
	r := httpCache.Put(request("", testURL), response(200), []byte("bad"))
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "request")
}

func TestCache_HTTPCache_Put_Ugly(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-put-ugly", "app-v1")
	r := httpCache.Put(request("GET", testURL), response(99), []byte("bad"))
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "response")
}

func TestCache_HTTPCache_ReadBody_Good(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-body-good", "app-v1")
	req := request("GET", testURL)
	RequireTrue(t, httpCache.Put(req, response(200), []byte("body")).OK)
	resp := httpCache.Match(req).Value.(*cache.CachedResponse)
	AssertEqual(t, "body", string(resultBytes(t, httpCache.ReadBody(resp))))
}

func TestCache_HTTPCache_ReadBody_Bad(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-body-bad", "app-v1")
	r := httpCache.ReadBody(nil)
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "nil")
}

func TestCache_HTTPCache_ReadBody_Ugly(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-body-ugly", "app-v1")
	r := httpCache.ReadBody(&cache.CachedResponse{BodyPath: "../escape.bin"})
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "body path")
}

func TestCache_HTTPCache_Delete_Good(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-delete-good", "app-v1")
	req := request("GET", testURL)
	RequireTrue(t, httpCache.Put(req, response(200), []byte("body")).OK)
	RequireTrue(t, httpCache.Delete(req).OK)
	AssertNil(t, httpCache.Match(req).Value)
}

func TestCache_HTTPCache_Delete_Bad(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-delete-bad", "app-v1")
	r := httpCache.Delete(request("", testURL))
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "request")
}

func TestCache_HTTPCache_Delete_Ugly(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-delete-ugly", "app-v1")
	r := httpCache.Delete(request("GET", testURL))
	AssertTrue(t, r.OK)
	AssertNil(t, r.Value)
}

func TestCache_HTTPCache_Keys_Good(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-keys-good", "app-v1")
	RequireTrue(t, httpCache.Put(request("GET", testURL), response(200), []byte("body")).OK)
	keys := httpCache.Keys().Value.([]string)
	AssertEqual(t, []string{testURL}, keys)
}

func TestCache_HTTPCache_Keys_Bad(t *T) {
	scripted := newScriptedMedium()
	storage := cache.NewCacheStorage(scripted, testBase+"/http-keys-bad").Value.(*cache.CacheStorage)
	httpCache := storage.Open("app-v1").Value.(*cache.HTTPCache)
	scripted.listErr[testBase+"/http-keys-bad/app-v1/responses"] = AnError
	r := httpCache.Keys()
	AssertFalse(t, r.OK)
	AssertContains(t, r.Error(), "list")
}

func TestCache_HTTPCache_Keys_Ugly(t *T) {
	httpCache, _ := testHTTPCache(t, testBase+"/http-keys-ugly", "app-v1")
	keys := httpCache.Keys().Value.([]string)
	AssertEqual(t, 0, len(keys))
}
