// SPDX-License-Identifier: EUPL-1.2

package cache_test

import (
	"time"

	. "dappco.re/go"
	"dappco.re/go/cache"
	coreio "dappco.re/go/io"
)

func ax7Cache(t *T, baseDir string) (*cache.Cache, *coreio.MockMedium) {
	t.Helper()
	return newTestCache(t, baseDir, time.Minute)
}

func ax7Storage(t *T, baseDir string) (*cache.CacheStorage, *coreio.MockMedium) {
	t.Helper()

	medium := coreio.NewMockMedium()
	storage, err := cache.NewCacheStorage(medium, baseDir)
	RequireNoError(t, err)
	return storage, medium
}

func ax7HTTPCache(t *T, baseDir, name string) (*cache.HTTPCache, *coreio.MockMedium) {
	t.Helper()

	storage, medium := ax7Storage(t, baseDir)
	httpCache, err := storage.Open(name)
	RequireNoError(t, err)
	return httpCache, medium
}

func ax7Request(method, url string) cache.CachedRequest {
	return cache.CachedRequest{Method: method, URL: url}
}

func ax7Response(status int) cache.CachedResponse {
	return cache.CachedResponse{
		Status:     status,
		StatusText: "OK",
		Headers:    map[string]string{"Content-Type": "text/plain"},
	}
}

func TestCache_New_Ugly(t *T) {
	dir := t.TempDir()
	c, err := cache.New(nil, dir, 0)

	AssertNoError(t, err)
	AssertNotNil(t, c)
	AssertNoError(t, c.Set("agent/defaults", "ready"))
}

func TestCache_NewCacheStorage_Ugly(t *T) {
	dir := t.TempDir()
	storage, err := cache.NewCacheStorage(nil, dir)

	AssertNoError(t, err)
	AssertNotNil(t, storage)
	AssertNoError(t, storage.Close())
}

func TestCache_GitHubReposKey_Bad(t *T) {
	key := cache.GitHubReposKey("acme/widgets")

	AssertContains(t, key, "acme%2Fwidgets")
	AssertNotContains(t, key, "acme/widgets")
	AssertContains(t, key, "repos")
}

func TestCache_GitHubReposKey_Ugly(t *T) {
	key := cache.GitHubReposKey("")

	AssertEqual(t, "github//repos", key)
	AssertContains(t, key, "github")
	AssertContains(t, key, "repos")
}

func TestCache_GitHubRepoKey_Bad(t *T) {
	key := cache.GitHubRepoKey("acme/widgets", "api server")

	AssertContains(t, key, "acme%2Fwidgets")
	AssertContains(t, key, "api%20server")
	AssertNotContains(t, key, "api server")
}

func TestCache_GitHubRepoKey_Ugly(t *T) {
	key := cache.GitHubRepoKey("", "")

	AssertEqual(t, "github///meta", key)
	AssertContains(t, key, "github")
	AssertContains(t, key, "meta")
}

func TestCache_JSON_MarshalJSON_Good(t *T) {
	var entry cache.Entry
	r := JSONUnmarshalString(`{"data":{"agent":"codex"}}`, &entry)
	RequireTrue(t, r.OK)

	out := JSONMarshalString(entry)
	AssertContains(t, out, `"data":{"agent":"codex"}`)
}

func TestCache_JSON_MarshalJSON_Bad(t *T) {
	entry := cache.Entry{}
	out := JSONMarshalString(entry)

	AssertContains(t, out, `"data":null`)
	AssertContains(t, out, `"cached_at"`)
	AssertContains(t, out, `"expires_at"`)
}

func TestCache_JSON_MarshalJSON_Ugly(t *T) {
	var entry cache.Entry
	r := JSONUnmarshalString(`{"data":[1,true,null]}`, &entry)
	RequireTrue(t, r.OK)

	out := JSONMarshalString(entry)
	AssertContains(t, out, `"data":[1,true,null]`)
}

func TestCache_JSON_UnmarshalJSON_Good(t *T) {
	var entry cache.Entry
	r := JSONUnmarshalString(`{"data":{"agent":"codex"}}`, &entry)

	AssertTrue(t, r.OK)
	AssertContains(t, JSONMarshalString(entry), `"agent":"codex"`)
	AssertNotContains(t, JSONMarshalString(entry), "eyJhZ2VudCI")
}

func TestCache_JSON_UnmarshalJSON_Bad(t *T) {
	var entry cache.Entry
	r := JSONUnmarshalString(`{"data":`, &entry)

	AssertFalse(t, r.OK)
	AssertNotNil(t, r.Value)
	AssertEqual(t, "null", JSONMarshalString(entry.Data))
}

func TestCache_JSON_UnmarshalJSON_Ugly(t *T) {
	var entry cache.Entry
	r := JSONUnmarshalString(`{"data":null}`, &entry)

	AssertTrue(t, r.OK)
	AssertContains(t, JSONMarshalString(entry), `"data":null`)
	AssertNotContains(t, JSONMarshalString(entry), `"data":""`)
}

func TestCache_Cache_Path_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-path-good")
	path, err := c.Path("agent/session")

	AssertNoError(t, err)
	AssertEqual(t, "/tmp/ax7-cache-path-good/agent/session.json", path)
	AssertContains(t, path, "agent/session.json")
}

func TestCache_Cache_Path_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-path-bad")
	path, err := c.Path("../escape")

	AssertError(t, err)
	AssertEqual(t, "", path)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_Cache_Path_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-path-ugly")
	path, err := c.Path(repeatString("a", 4097))

	AssertError(t, err)
	AssertEqual(t, "", path)
	AssertContains(t, err.Error(), "too long")
}

func TestCache_Cache_Get_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-get-good")
	RequireNoError(t, c.Set("agent/profile", map[string]string{"name": "codex"}))

	var got map[string]string
	found, err := c.Get("agent/profile", &got)
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, "codex", got["name"])
}

func TestCache_Cache_Get_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-get-bad")
	var got map[string]string

	found, err := c.Get("agent/missing", &got)
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_Cache_Get_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-get-ugly")
	RequireNoError(t, c.Set("agent/profile", map[string]string{"name": "codex"}))

	found, err := c.Get("agent/profile", nil)
	AssertError(t, err)
	AssertFalse(t, found)
	AssertContains(t, err.Error(), "unmarshal")
}

func TestCache_Cache_Set_Good(t *T) {
	c, medium := ax7Cache(t, "/tmp/ax7-cache-set-good")
	err := c.Set("agent/profile", map[string]string{"name": "codex"})
	RequireNoError(t, err)

	raw, err := medium.Read("/tmp/ax7-cache-set-good/agent/profile.json")
	AssertNoError(t, err)
	AssertContains(t, raw, `"name": "codex"`)
}

func TestCache_Cache_Set_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-set-bad")
	err := c.Set("", "missing-key")

	AssertError(t, err)
	AssertContains(t, err.Error(), "empty")
}

func TestCache_Cache_Set_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-set-ugly")
	err := c.Set("agent/handler", map[string]any{"fn": func() {}})

	AssertError(t, err)
	AssertContains(t, err.Error(), "marshal")
}

func TestCache_Cache_SetWithTTL_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setttl-good")
	RequireNoError(t, c.SetWithTTL("agent/profile", "codex", time.Minute))

	var got string
	found, err := c.Get("agent/profile", &got)
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, "codex", got)
}

func TestCache_Cache_SetWithTTL_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setttl-bad")
	err := c.SetWithTTL("agent/profile", "codex", -time.Second)

	AssertError(t, err)
	AssertContains(t, err.Error(), "ttl")
}

func TestCache_Cache_SetWithTTL_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setttl-ugly")
	RequireNoError(t, c.SetWithTTL("agent/profile", "codex", 0))

	var got string
	found, err := c.Get("agent/profile", &got)
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertEqual(t, "", got)
}

func TestCache_Cache_SetBinary_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setbinary-good")
	RequireNoError(t, c.SetBinary("artifact/blob", []byte("payload"), "text/plain"))

	got, found, err := c.GetBinary("artifact/blob")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, []byte("payload"), got)
}

func TestCache_Cache_SetBinary_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setbinary-bad")
	err := c.SetBinary("../escape", []byte("payload"), "text/plain")

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_Cache_SetBinary_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setbinary-ugly")
	RequireNoError(t, c.SetBinary("artifact/empty", []byte{}, ""))

	got, found, err := c.GetBinary("artifact/empty")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEmpty(t, got)
}

func TestCache_Cache_SetBinaryWithTTL_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setbinaryttl-good")
	RequireNoError(t, c.SetBinaryWithTTL("artifact/blob", []byte("payload"), "text/plain", time.Minute))

	got, found, err := c.GetBinary("artifact/blob")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, []byte("payload"), got)
}

func TestCache_Cache_SetBinaryWithTTL_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setbinaryttl-bad")
	err := c.SetBinaryWithTTL("artifact/blob", []byte("payload"), "text/plain", -time.Second)

	AssertError(t, err)
	AssertContains(t, err.Error(), "ttl")
}

func TestCache_Cache_SetBinaryWithTTL_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-setbinaryttl-ugly")
	RequireNoError(t, c.SetBinaryWithTTL("artifact/blob", []byte("payload"), "text/plain", 0))

	got, found, err := c.GetBinary("artifact/blob")
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_Cache_GetBinary_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-getbinary-good")
	RequireNoError(t, c.SetBinary("artifact/blob", []byte("payload"), "text/plain"))

	got, found, err := c.GetBinary("artifact/blob")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, "payload", string(got))
}

func TestCache_Cache_GetBinary_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-getbinary-bad")

	got, found, err := c.GetBinary("artifact/missing")
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_Cache_GetBinary_Ugly(t *T) {
	c, medium := ax7Cache(t, "/tmp/ax7-cache-getbinary-ugly")
	RequireNoError(t, medium.Write("/tmp/ax7-cache-getbinary-ugly/artifact/blob.json", "{"))

	got, found, err := c.GetBinary("artifact/blob")
	AssertError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_Cache_Delete_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-delete-good")
	RequireNoError(t, c.Set("agent/profile", "codex"))
	RequireNoError(t, c.Delete("agent/profile"))

	var got string
	found, err := c.Get("agent/profile", &got)
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertEqual(t, "", got)
}

func TestCache_Cache_Delete_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-delete-bad")
	err := c.Delete("../escape")

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_Cache_Delete_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-delete-ugly")
	err := c.Delete("agent/missing")

	AssertNoError(t, err)
	secondErr := c.Delete("agent/missing")
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), c.Age("agent/missing"))
}

func TestCache_Cache_DeleteMany_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-deletemany-good")
	RequireNoError(t, c.Set("agent/one", "one"))
	RequireNoError(t, c.Set("agent/two", "two"))

	err := c.DeleteMany("agent/one", "agent/two")
	AssertNoError(t, err)
	AssertEqual(t, time.Duration(-1), c.Age("agent/one"))
	AssertEqual(t, time.Duration(-1), c.Age("agent/two"))
}

func TestCache_Cache_DeleteMany_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-deletemany-bad")
	RequireNoError(t, c.Set("agent/keep", "value"))

	err := c.DeleteMany("agent/keep", "../escape")
	AssertError(t, err)
	AssertGreaterOrEqual(t, c.Age("agent/keep"), time.Duration(0))
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_Cache_DeleteMany_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-deletemany-ugly")
	err := c.DeleteMany()

	AssertNoError(t, err)
	secondErr := c.DeleteMany()
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), c.Age("agent/missing"))
}

func TestCache_Cache_Clear_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-clear-good")
	RequireNoError(t, c.Set("agent/profile", "codex"))
	RequireNoError(t, c.Clear())

	var got string
	found, err := c.Get("agent/profile", &got)
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertEqual(t, "", got)
}

func TestCache_Cache_Clear_Bad(t *T) {
	medium := newScriptedMedium()
	medium.deleteAllErr["/tmp/ax7-cache-clear-bad"] = NewError("blocked")
	c, err := cache.New(medium, "/tmp/ax7-cache-clear-bad", time.Minute)
	RequireNoError(t, err)

	err = c.Clear()
	AssertError(t, err)
	AssertContains(t, err.Error(), "blocked")
}

func TestCache_Cache_Clear_Ugly(t *T) {
	var c cache.Cache
	err := c.Clear()

	AssertError(t, err)
	AssertContains(t, err.Error(), "base directory")
}

func TestCache_Cache_Age_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-age-good")
	RequireNoError(t, c.Set("agent/profile", "codex"))

	age := c.Age("agent/profile")
	AssertGreaterOrEqual(t, age, time.Duration(0))
	AssertLess(t, age, time.Minute)
}

func TestCache_Cache_Age_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-age-bad")
	age := c.Age("agent/missing")

	AssertEqual(t, time.Duration(-1), age)
	AssertLess(t, age, time.Duration(0))
}

func TestCache_Cache_Age_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-age-ugly")
	age := c.Age("../escape")

	AssertEqual(t, time.Duration(-1), age)
	AssertLess(t, age, time.Duration(0))
}

func TestCache_Cache_OnInvalidate_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-oninvalidate-good")
	RequireNoError(t, c.Set("dns/a", "record"))
	c.OnInvalidate("flush", func(trigger string) []string { return []string{"dns/*"} })

	deleted, err := c.Invalidate("flush")
	AssertNoError(t, err)
	AssertEqual(t, 1, deleted)
}

func TestCache_Cache_OnInvalidate_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-oninvalidate-bad")
	RequireNoError(t, c.Set("dns/a", "record"))
	c.OnInvalidate("flush", nil)

	deleted, err := c.Invalidate("flush")
	AssertNoError(t, err)
	AssertEqual(t, 0, deleted)
}

func TestCache_Cache_OnInvalidate_Ugly(t *T) {
	var c *cache.Cache
	AssertNotPanics(t, func() {
		c.OnInvalidate("flush", func(string) []string { return []string{"dns/*"} })
	})
	AssertNil(t, c)
}

func TestCache_Cache_Invalidate_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-invalidate-good")
	RequireNoError(t, c.Set("dns/a", "record"))
	c.OnInvalidate("flush", func(string) []string { return []string{"dns/*"} })

	deleted, err := c.Invalidate("flush")
	AssertNoError(t, err)
	AssertEqual(t, 1, deleted)
}

func TestCache_Cache_Invalidate_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-invalidate-bad")
	RequireNoError(t, c.Set("dns/a", "record"))

	deleted, err := c.Invalidate("missing")
	AssertNoError(t, err)
	AssertEqual(t, 0, deleted)
	AssertGreaterOrEqual(t, c.Age("dns/a"), time.Duration(0))
}

func TestCache_Cache_Invalidate_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-invalidate-ugly")
	c.OnInvalidate("flush", func(string) []string { return []string{repeatString("a", 4097)} })

	deleted, err := c.Invalidate("flush")
	AssertError(t, err)
	AssertEqual(t, 0, deleted)
	AssertContains(t, err.Error(), "too long")
}

func TestCache_Cache_Scoped_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-scoped-good")
	scoped := c.Scoped("https://app.example")

	AssertNotNil(t, scoped)
	AssertNoError(t, scoped.Set("profile", "codex"))
	AssertGreaterOrEqual(t, scoped.Age("profile"), time.Duration(0))
}

func TestCache_Cache_Scoped_Bad(t *T) {
	var c *cache.Cache
	scoped := c.Scoped("https://app.example")

	AssertNil(t, scoped)
	AssertNil(t, c)
}

func TestCache_Cache_Scoped_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-scoped-ugly")
	scoped := c.Scoped("")

	AssertNotNil(t, scoped)
	AssertNoError(t, scoped.Set("empty-origin", "codex"))
	AssertGreaterOrEqual(t, scoped.Age("empty-origin"), time.Duration(0))
}

func TestCache_Cache_ClearScope_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-clearscope-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("profile", "codex"))

	err := c.ClearScope("https://app.example")
	AssertNoError(t, err)
	AssertEqual(t, time.Duration(-1), scoped.Age("profile"))
}

func TestCache_Cache_ClearScope_Bad(t *T) {
	var c *cache.Cache
	err := c.ClearScope("https://app.example")

	AssertError(t, err)
	AssertContains(t, err.Error(), "nil")
}

func TestCache_Cache_ClearScope_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-cache-clearscope-ugly")
	err := c.ClearScope("https://missing.example")

	AssertNoError(t, err)
	secondErr := c.ClearScope("")
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), c.Age("scope_missing"))
}

func TestCache_ScopedCache_Path_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-path-good")
	scoped := c.Scoped("https://app.example")

	path, err := scoped.Path("profile")
	AssertNoError(t, err)
	AssertContains(t, path, "scope_")
	AssertContains(t, path, "/profile.json")
}

func TestCache_ScopedCache_Path_Bad(t *T) {
	var scoped *cache.ScopedCache
	path, err := scoped.Path("profile")

	AssertError(t, err)
	AssertEqual(t, "", path)
	AssertContains(t, err.Error(), "nil")
}

func TestCache_ScopedCache_Path_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-path-ugly")
	scoped := c.Scoped("https://app.example")

	path, err := scoped.Path("../escape")
	AssertError(t, err)
	AssertEqual(t, "", path)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_ScopedCache_Get_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-get-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("profile", "codex"))

	var got string
	found, err := scoped.Get("profile", &got)
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, "codex", got)
}

func TestCache_ScopedCache_Get_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-get-bad")
	scoped := c.Scoped("https://app.example")
	var got string

	found, err := scoped.Get("missing", &got)
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertEqual(t, "", got)
}

func TestCache_ScopedCache_Get_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-get-ugly")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("profile", "codex"))

	found, err := scoped.Get("profile", nil)
	AssertError(t, err)
	AssertFalse(t, found)
	AssertContains(t, err.Error(), "unmarshal")
}

func TestCache_ScopedCache_Set_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-set-good")
	scoped := c.Scoped("https://app.example")
	err := scoped.Set("profile", "codex")

	AssertNoError(t, err)
	AssertGreaterOrEqual(t, scoped.Age("profile"), time.Duration(0))
	AssertEqual(t, time.Duration(-1), c.Age("profile"))
}

func TestCache_ScopedCache_Set_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-set-bad")
	scoped := c.Scoped("https://app.example")
	err := scoped.Set("../escape", "codex")

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
	AssertEqual(t, time.Duration(-1), scoped.Age("../escape"))
}

func TestCache_ScopedCache_Set_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-set-ugly")
	scoped := c.Scoped("https://app.example")
	err := scoped.Set("handler", map[string]any{"fn": func() {}})

	AssertError(t, err)
	AssertContains(t, err.Error(), "marshal")
	AssertEqual(t, time.Duration(-1), scoped.Age("handler"))
}

func TestCache_ScopedCache_SetWithTTL_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setttl-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetWithTTL("profile", "codex", time.Minute))

	var got string
	found, err := scoped.Get("profile", &got)
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, "codex", got)
}

func TestCache_ScopedCache_SetWithTTL_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setttl-bad")
	scoped := c.Scoped("https://app.example")
	err := scoped.SetWithTTL("profile", "codex", -time.Second)

	AssertError(t, err)
	AssertContains(t, err.Error(), "ttl")
	AssertEqual(t, time.Duration(-1), scoped.Age("profile"))
}

func TestCache_ScopedCache_SetWithTTL_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setttl-ugly")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetWithTTL("profile", "codex", 0))

	var got string
	found, err := scoped.Get("profile", &got)
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertEqual(t, "", got)
}

func TestCache_ScopedCache_SetBinary_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setbinary-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetBinary("artifact", []byte("payload"), "text/plain"))

	got, found, err := scoped.GetBinary("artifact")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, []byte("payload"), got)
}

func TestCache_ScopedCache_SetBinary_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setbinary-bad")
	scoped := c.Scoped("https://app.example")
	err := scoped.SetBinary("../escape", []byte("payload"), "text/plain")

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
	AssertEqual(t, time.Duration(-1), scoped.Age("../escape"))
}

func TestCache_ScopedCache_SetBinary_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setbinary-ugly")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetBinary("artifact", []byte{}, ""))

	got, found, err := scoped.GetBinary("artifact")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEmpty(t, got)
}

func TestCache_ScopedCache_SetBinaryWithTTL_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setbinaryttl-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetBinaryWithTTL("artifact", []byte("payload"), "text/plain", time.Minute))

	got, found, err := scoped.GetBinary("artifact")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, []byte("payload"), got)
}

func TestCache_ScopedCache_SetBinaryWithTTL_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setbinaryttl-bad")
	scoped := c.Scoped("https://app.example")
	err := scoped.SetBinaryWithTTL("artifact", []byte("payload"), "text/plain", -time.Second)

	AssertError(t, err)
	AssertContains(t, err.Error(), "ttl")
	AssertEqual(t, time.Duration(-1), scoped.Age("artifact"))
}

func TestCache_ScopedCache_SetBinaryWithTTL_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-setbinaryttl-ugly")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetBinaryWithTTL("artifact", []byte("payload"), "text/plain", 0))

	got, found, err := scoped.GetBinary("artifact")
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_ScopedCache_GetBinary_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-getbinary-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.SetBinary("artifact", []byte("payload"), "text/plain"))

	got, found, err := scoped.GetBinary("artifact")
	AssertNoError(t, err)
	AssertTrue(t, found)
	AssertEqual(t, "payload", string(got))
}

func TestCache_ScopedCache_GetBinary_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-getbinary-bad")
	scoped := c.Scoped("https://app.example")

	got, found, err := scoped.GetBinary("missing")
	AssertNoError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_ScopedCache_GetBinary_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-getbinary-ugly")
	scoped := c.Scoped("https://app.example")

	got, found, err := scoped.GetBinary("../escape")
	AssertError(t, err)
	AssertFalse(t, found)
	AssertNil(t, got)
}

func TestCache_ScopedCache_Delete_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-delete-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("profile", "codex"))

	err := scoped.Delete("profile")
	AssertNoError(t, err)
	AssertEqual(t, time.Duration(-1), scoped.Age("profile"))
}

func TestCache_ScopedCache_Delete_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-delete-bad")
	scoped := c.Scoped("https://app.example")
	err := scoped.Delete("../escape")

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
	AssertEqual(t, time.Duration(-1), scoped.Age("../escape"))
}

func TestCache_ScopedCache_Delete_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-delete-ugly")
	scoped := c.Scoped("https://app.example")
	err := scoped.Delete("missing")

	AssertNoError(t, err)
	secondErr := scoped.Delete("missing")
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), scoped.Age("missing"))
}

func TestCache_ScopedCache_DeleteMany_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-deletemany-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("one", "1"))
	RequireNoError(t, scoped.Set("two", "2"))

	err := scoped.DeleteMany("one", "two")
	AssertNoError(t, err)
	AssertEqual(t, time.Duration(-1), scoped.Age("one"))
	AssertEqual(t, time.Duration(-1), scoped.Age("two"))
}

func TestCache_ScopedCache_DeleteMany_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-deletemany-bad")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("keep", "value"))

	err := scoped.DeleteMany("keep", "../escape")
	AssertError(t, err)
	AssertGreaterOrEqual(t, scoped.Age("keep"), time.Duration(0))
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_ScopedCache_DeleteMany_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-deletemany-ugly")
	scoped := c.Scoped("https://app.example")
	err := scoped.DeleteMany()

	AssertNoError(t, err)
	secondErr := scoped.DeleteMany()
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), scoped.Age("missing"))
}

func TestCache_ScopedCache_Clear_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-clear-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("profile", "codex"))
	RequireNoError(t, c.Set("profile", "root"))

	err := scoped.Clear()
	AssertNoError(t, err)
	AssertEqual(t, time.Duration(-1), scoped.Age("profile"))
	AssertGreaterOrEqual(t, c.Age("profile"), time.Duration(0))
}

func TestCache_ScopedCache_Clear_Bad(t *T) {
	var scoped *cache.ScopedCache
	err := scoped.Clear()

	AssertError(t, err)
	AssertContains(t, err.Error(), "nil")
}

func TestCache_ScopedCache_Clear_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-clear-ugly")
	scoped := c.Scoped("https://app.example")
	err := scoped.Clear()

	AssertNoError(t, err)
	secondErr := scoped.Clear()
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), scoped.Age("missing"))
}

func TestCache_ScopedCache_ClearScope_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-clearscope-good")
	scoped := c.Scoped("https://app.example")
	admin := scoped.Scoped("https://admin.example")
	RequireNoError(t, admin.Set("profile", "admin"))

	err := scoped.ClearScope("https://admin.example")
	AssertNoError(t, err)
	AssertEqual(t, time.Duration(-1), admin.Age("profile"))
}

func TestCache_ScopedCache_ClearScope_Bad(t *T) {
	var scoped *cache.ScopedCache
	err := scoped.ClearScope("https://admin.example")

	AssertError(t, err)
	AssertContains(t, err.Error(), "nil")
}

func TestCache_ScopedCache_ClearScope_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-clearscope-ugly")
	scoped := c.Scoped("https://app.example")
	err := scoped.ClearScope("")

	AssertNoError(t, err)
	secondErr := scoped.ClearScope("")
	AssertNoError(t, secondErr)
	AssertEqual(t, time.Duration(-1), scoped.Age("missing"))
}

func TestCache_ScopedCache_OnInvalidate_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-oninvalidate-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("prefs/theme", "dark"))
	scoped.OnInvalidate("flush", func(string) []string { return []string{"prefs/*"} })

	deleted, err := c.Invalidate("flush")
	AssertNoError(t, err)
	AssertEqual(t, 1, deleted)
}

func TestCache_ScopedCache_OnInvalidate_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-oninvalidate-bad")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("prefs/theme", "dark"))
	scoped.OnInvalidate("flush", nil)

	deleted, err := c.Invalidate("flush")
	AssertNoError(t, err)
	AssertEqual(t, 0, deleted)
}

func TestCache_ScopedCache_OnInvalidate_Ugly(t *T) {
	var scoped *cache.ScopedCache
	AssertNotPanics(t, func() {
		scoped.OnInvalidate("flush", func(string) []string { return []string{"prefs/*"} })
	})
	AssertNil(t, scoped)
}

func TestCache_ScopedCache_Invalidate_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-invalidate-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("prefs/theme", "dark"))
	scoped.OnInvalidate("flush", func(string) []string { return []string{"prefs/*"} })

	deleted, err := scoped.Invalidate("flush")
	AssertNoError(t, err)
	AssertEqual(t, 1, deleted)
}

func TestCache_ScopedCache_Invalidate_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-invalidate-bad")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("prefs/theme", "dark"))

	deleted, err := scoped.Invalidate("missing")
	AssertNoError(t, err)
	AssertEqual(t, 0, deleted)
}

func TestCache_ScopedCache_Invalidate_Ugly(t *T) {
	var scoped *cache.ScopedCache
	deleted, err := scoped.Invalidate("flush")

	AssertError(t, err)
	AssertEqual(t, 0, deleted)
	AssertContains(t, err.Error(), "nil")
}

func TestCache_ScopedCache_Age_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-age-good")
	scoped := c.Scoped("https://app.example")
	RequireNoError(t, scoped.Set("profile", "codex"))

	age := scoped.Age("profile")
	AssertGreaterOrEqual(t, age, time.Duration(0))
	AssertLess(t, age, time.Minute)
}

func TestCache_ScopedCache_Age_Bad(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-age-bad")
	scoped := c.Scoped("https://app.example")
	age := scoped.Age("missing")

	AssertEqual(t, time.Duration(-1), age)
	AssertLess(t, age, time.Duration(0))
}

func TestCache_ScopedCache_Age_Ugly(t *T) {
	var scoped *cache.ScopedCache
	age := scoped.Age("profile")

	AssertEqual(t, time.Duration(-1), age)
	AssertLess(t, age, time.Duration(0))
}

func TestCache_ScopedCache_Scoped_Good(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-scoped-good")
	scoped := c.Scoped("https://app.example")
	admin := scoped.Scoped("https://admin.example")

	AssertNotNil(t, admin)
	AssertNoError(t, admin.Set("profile", "admin"))
	AssertEqual(t, time.Duration(-1), scoped.Age("profile"))
}

func TestCache_ScopedCache_Scoped_Bad(t *T) {
	var scoped *cache.ScopedCache
	admin := scoped.Scoped("https://admin.example")

	AssertNil(t, admin)
	AssertNil(t, scoped)
}

func TestCache_ScopedCache_Scoped_Ugly(t *T) {
	c, _ := ax7Cache(t, "/tmp/ax7-scoped-scoped-ugly")
	scoped := c.Scoped("https://app.example")
	empty := scoped.Scoped("")

	AssertNotNil(t, empty)
	AssertNoError(t, empty.Set("profile", "empty"))
	AssertGreaterOrEqual(t, empty.Age("profile"), time.Duration(0))
}

func TestCache_CacheStorage_Open_Good(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-open-good")
	first, err := storage.Open("static")
	RequireNoError(t, err)

	second, err := storage.Open("static")
	AssertNoError(t, err)
	AssertSame(t, first, second)
	AssertNotNil(t, second)
}

func TestCache_CacheStorage_Open_Bad(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-open-bad")
	httpCache, err := storage.Open("../escape")

	AssertError(t, err)
	AssertNil(t, httpCache)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_CacheStorage_Open_Ugly(t *T) {
	var storage cache.CacheStorage
	httpCache, err := storage.Open("static")

	AssertError(t, err)
	AssertNil(t, httpCache)
	AssertContains(t, err.Error(), "medium")
}

func TestCache_CacheStorage_Delete_Good(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-delete-good")
	_, err := storage.Open("static")
	RequireNoError(t, err)

	err = storage.Delete("static")
	AssertNoError(t, err)
	keys, err := storage.Keys()
	AssertNoError(t, err)
	AssertNotContains(t, keys, "static")
}

func TestCache_CacheStorage_Delete_Bad(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-delete-bad")
	err := storage.Delete("../escape")

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_CacheStorage_Delete_Ugly(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-delete-ugly")
	err := storage.Delete("missing")

	AssertNoError(t, err)
	keys, err := storage.Keys()
	AssertNoError(t, err)
	AssertEmpty(t, keys)
}

func TestCache_CacheStorage_Keys_Good(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-keys-good")
	_, err := storage.Open("api")
	RequireNoError(t, err)
	_, err = storage.Open("static")
	RequireNoError(t, err)

	keys, err := storage.Keys()
	AssertNoError(t, err)
	AssertEqual(t, []string{"api", "static"}, keys)
}

func TestCache_CacheStorage_Keys_Bad(t *T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/ax7-storage-keys-bad")
	RequireNoError(t, err)
	medium.listErr["/tmp/ax7-storage-keys-bad"] = NewError("list failed")

	keys, err := storage.Keys()
	AssertError(t, err)
	AssertNil(t, keys)
	AssertContains(t, err.Error(), "list failed")
}

func TestCache_CacheStorage_Keys_Ugly(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-keys-ugly")
	_, err := storage.Open("static")
	RequireNoError(t, err)
	RequireNoError(t, storage.Close())

	keys, err := storage.Keys()
	AssertNoError(t, err)
	AssertContains(t, keys, "static")
}

func TestCache_CacheStorage_Close_Good(t *T) {
	storage, _ := ax7Storage(t, "/tmp/ax7-storage-close-good")
	_, err := storage.Open("static")
	RequireNoError(t, err)

	err = storage.Close()
	AssertNoError(t, err)
	secondErr := storage.Close()
	AssertNoError(t, secondErr)
}

func TestCache_CacheStorage_Close_Bad(t *T) {
	var storage *cache.CacheStorage
	err := storage.Close()

	AssertNoError(t, err)
	AssertNil(t, storage)
	secondErr := storage.Close()
	AssertNoError(t, secondErr)
}

func TestCache_CacheStorage_Close_Ugly(t *T) {
	var storage cache.CacheStorage
	err := storage.Close()

	AssertNoError(t, err)
	_, err = storage.Keys()
	AssertError(t, err)
}

func TestCache_HTTPCache_Match_Good(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-match-good", "api")
	req := ax7Request("GET", "https://example.com/data")
	RequireNoError(t, httpCache.Put(req, ax7Response(200), []byte("body")))

	resp, err := httpCache.Match(req)
	AssertNoError(t, err)
	AssertNotNil(t, resp)
	AssertEqual(t, 200, resp.Status)
}

func TestCache_HTTPCache_Match_Ugly(t *T) {
	httpCache, medium := ax7HTTPCache(t, "/tmp/ax7-http-match-ugly", "api")
	req := ax7Request("GET", "https://example.com/legacy")
	key := legacyHTTPCacheStorageKey(req)
	resp := ax7Response(203)
	resp.BodyPath = JoinPath("responses", key+".bin")
	RequireNoError(t, medium.Write(JoinPath("/tmp/ax7-http-match-ugly", "api", "responses", key+".json"), JSONMarshalString(resp)))
	RequireNoError(t, medium.Write(JoinPath("/tmp/ax7-http-match-ugly", "api", "responses", key+".bin"), "legacy"))

	got, err := httpCache.Match(req)
	AssertNoError(t, err)
	AssertEqual(t, 203, got.Status)
}

func TestCache_HTTPCache_Put_Good(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-put-good", "api")
	req := ax7Request("POST", "https://example.com/data")
	err := httpCache.Put(req, ax7Response(201), []byte("created"))

	AssertNoError(t, err)
	resp, err := httpCache.Match(req)
	AssertNoError(t, err)
	AssertEqual(t, 201, resp.Status)
}

func TestCache_HTTPCache_ReadBody_Good(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-readbody-good", "api")
	req := ax7Request("GET", "https://example.com/data")
	RequireNoError(t, httpCache.Put(req, ax7Response(200), []byte("body")))
	resp, err := httpCache.Match(req)
	RequireNoError(t, err)

	body, err := httpCache.ReadBody(resp)
	AssertNoError(t, err)
	AssertEqual(t, []byte("body"), body)
}

func TestCache_HTTPCache_ReadBody_Bad(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-readbody-bad", "api")
	body, err := httpCache.ReadBody(nil)

	AssertError(t, err)
	AssertNil(t, body)
	AssertContains(t, err.Error(), "nil")
}

func TestCache_HTTPCache_ReadBody_Ugly(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-readbody-ugly", "api")
	resp := &cache.CachedResponse{BodyPath: "../escape.bin", Status: 200}

	body, err := httpCache.ReadBody(resp)
	AssertError(t, err)
	AssertNil(t, body)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_HTTPCache_Delete_Good(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-delete-good", "api")
	req := ax7Request("GET", "https://example.com/data")
	RequireNoError(t, httpCache.Put(req, ax7Response(200), []byte("body")))

	err := httpCache.Delete(req)
	AssertNoError(t, err)
	resp, err := httpCache.Match(req)
	AssertNoError(t, err)
	AssertNil(t, resp)
}

func TestCache_HTTPCache_Delete_Bad(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-delete-bad", "api")
	err := httpCache.Delete(cache.CachedRequest{})

	AssertError(t, err)
	AssertContains(t, err.Error(), "invalid")
}

func TestCache_HTTPCache_Delete_Ugly(t *T) {
	httpCache, _ := ax7HTTPCache(t, "/tmp/ax7-http-delete-ugly", "api")
	req := ax7Request("GET", "https://example.com/missing")
	err := httpCache.Delete(req)

	AssertNoError(t, err)
	resp, err := httpCache.Match(req)
	AssertNoError(t, err)
	AssertNil(t, resp)
}

func TestCache_HTTPCache_Keys_Bad(t *T) {
	medium := newScriptedMedium()
	storage, err := cache.NewCacheStorage(medium, "/tmp/ax7-http-keys-bad")
	RequireNoError(t, err)
	httpCache, err := storage.Open("api")
	RequireNoError(t, err)
	medium.listErr["/tmp/ax7-http-keys-bad/api/responses"] = NewError("list failed")

	urls, err := httpCache.Keys()
	AssertError(t, err)
	AssertNil(t, urls)
	AssertContains(t, err.Error(), "list failed")
}

func TestCache_HTTPCache_Keys_Ugly(t *T) {
	httpCache, medium := ax7HTTPCache(t, "/tmp/ax7-http-keys-ugly", "api")
	RequireNoError(t, medium.Write("/tmp/ax7-http-keys-ugly/api/responses/bad.json", "{"))

	urls, err := httpCache.Keys()
	AssertNoError(t, err)
	AssertEmpty(t, urls)
	AssertNotContains(t, urls, "https://example.com")
}
