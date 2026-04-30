// SPDX-License-Identifier: EUPL-1.2

package cache_test

import (
	"time"

	"dappco.re/go/cache"
	coreio "dappco.re/go/io"
)

func exampleCache() *cache.Cache {
	return cache.New(coreio.NewMockMedium(), "/tmp/go-cache-example", time.Minute).Value.(*cache.Cache)
}

func exampleStorage() *cache.CacheStorage {
	return cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/go-cache-storage-example").Value.(*cache.CacheStorage)
}

func exampleHTTPCache() *cache.HTTPCache {
	storage := exampleStorage()
	return storage.Open("app-v1").Value.(*cache.HTTPCache)
}

func ExampleNew() {
	r := cache.New(coreio.NewMockMedium(), "/tmp/go-cache-example-new", cache.DefaultTTL)
	if r.OK {
		r.Value.(*cache.Cache).Set("agent/profile", map[string]string{"name": "codex"})
	}
}

func ExampleCache_Path() {
	c := exampleCache()
	c.Path("agent/profile")
}

func ExampleCache_Get() {
	c := exampleCache()
	c.Set("agent/profile", map[string]string{"name": "codex"})
	var profile map[string]string
	c.Get("agent/profile", &profile)
}

func ExampleCache_Set() {
	c := exampleCache()
	c.Set("agent/profile", map[string]string{"name": "codex"})
}

func ExampleCache_SetWithTTL() {
	c := exampleCache()
	c.SetWithTTL("agent/profile", "codex", time.Minute)
}

func ExampleCache_Delete() {
	c := exampleCache()
	c.Set("agent/profile", "codex")
	c.Delete("agent/profile")
}

func ExampleCache_SetBinary() {
	c := exampleCache()
	c.SetBinary("artifact/blob", []byte("data"), "application/octet-stream")
}

func ExampleCache_SetBinaryWithTTL() {
	c := exampleCache()
	c.SetBinaryWithTTL("artifact/blob", []byte("data"), "application/octet-stream", time.Minute)
}

func ExampleCache_GetBinary() {
	c := exampleCache()
	c.SetBinary("artifact/blob", []byte("data"), "application/octet-stream")
	c.GetBinary("artifact/blob")
}

func ExampleCache_DeleteMany() {
	c := exampleCache()
	c.Set("agent/one", "1")
	c.Set("agent/two", "2")
	c.DeleteMany("agent/one", "agent/two")
}

func ExampleCache_OnInvalidate() {
	c := exampleCache()
	c.OnInvalidate("agent.changed", func(string) []string { return []string{"agent/*"} })
}

func ExampleCache_Invalidate() {
	c := exampleCache()
	c.OnInvalidate("agent.changed", func(string) []string { return []string{"agent/*"} })
	c.Invalidate("agent.changed")
}

func ExampleCache_Scoped() {
	c := exampleCache()
	scoped := c.Scoped("https://app.example")
	scoped.Set("agent/profile", "codex")
}

func ExampleCache_ClearScope() {
	c := exampleCache()
	c.Scoped("https://app.example").Set("agent/profile", "codex")
	c.ClearScope("https://app.example")
}

func ExampleScopedCache_Scoped() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Scoped("https://admin.example")
}

func ExampleScopedCache_Path() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Path("agent/profile")
}

func ExampleScopedCache_Get() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Set("agent/profile", "codex")
	var profile string
	scoped.Get("agent/profile", &profile)
}

func ExampleScopedCache_Set() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Set("agent/profile", "codex")
}

func ExampleScopedCache_SetWithTTL() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.SetWithTTL("agent/profile", "codex", time.Minute)
}

func ExampleScopedCache_SetBinary() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.SetBinary("artifact/blob", []byte("data"), "application/octet-stream")
}

func ExampleScopedCache_SetBinaryWithTTL() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.SetBinaryWithTTL("artifact/blob", []byte("data"), "application/octet-stream", time.Minute)
}

func ExampleScopedCache_GetBinary() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.SetBinary("artifact/blob", []byte("data"), "application/octet-stream")
	scoped.GetBinary("artifact/blob")
}

func ExampleScopedCache_Delete() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Set("agent/profile", "codex")
	scoped.Delete("agent/profile")
}

func ExampleScopedCache_DeleteMany() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Set("agent/one", "1")
	scoped.Set("agent/two", "2")
	scoped.DeleteMany("agent/one", "agent/two")
}

func ExampleScopedCache_Clear() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Set("agent/profile", "codex")
	scoped.Clear()
}

func ExampleScopedCache_ClearScope() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Scoped("https://admin.example").Set("agent/profile", "codex")
	scoped.ClearScope("https://admin.example")
}

func ExampleScopedCache_OnInvalidate() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.OnInvalidate("agent.changed", func(string) []string { return []string{"agent/*"} })
}

func ExampleScopedCache_Invalidate() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.OnInvalidate("agent.changed", func(string) []string { return []string{"agent/*"} })
	scoped.Invalidate("agent.changed")
}

func ExampleScopedCache_Age() {
	scoped := exampleCache().Scoped("https://app.example")
	scoped.Set("agent/profile", "codex")
	scoped.Age("agent/profile")
}

func ExampleNewCacheStorage() {
	cache.NewCacheStorage(coreio.NewMockMedium(), "/tmp/go-cache-storage-example-new")
}

func ExampleCacheStorage_Open() {
	storage := exampleStorage()
	storage.Open("app-v1")
}

func ExampleCacheStorage_Delete() {
	storage := exampleStorage()
	storage.Open("app-v1")
	storage.Delete("app-v1")
}

func ExampleCacheStorage_Keys() {
	storage := exampleStorage()
	storage.Open("app-v1")
	storage.Keys()
}

func ExampleCacheStorage_Close() {
	storage := exampleStorage()
	storage.Close()
}

func ExampleHTTPCache_Match() {
	httpCache := exampleHTTPCache()
	req := cache.CachedRequest{Method: "GET", URL: "https://example.com/data"}
	httpCache.Put(req, cache.CachedResponse{Status: 200, StatusText: "OK"}, []byte("body"))
	httpCache.Match(req)
}

func ExampleHTTPCache_Put() {
	httpCache := exampleHTTPCache()
	req := cache.CachedRequest{Method: "GET", URL: "https://example.com/data"}
	httpCache.Put(req, cache.CachedResponse{Status: 200, StatusText: "OK"}, []byte("body"))
}

func ExampleHTTPCache_ReadBody() {
	httpCache := exampleHTTPCache()
	req := cache.CachedRequest{Method: "GET", URL: "https://example.com/data"}
	httpCache.Put(req, cache.CachedResponse{Status: 200, StatusText: "OK"}, []byte("body"))
	if r := httpCache.Match(req); r.OK && r.Value != nil {
		httpCache.ReadBody(r.Value.(*cache.CachedResponse))
	}
}

func ExampleHTTPCache_Delete() {
	httpCache := exampleHTTPCache()
	req := cache.CachedRequest{Method: "GET", URL: "https://example.com/data"}
	httpCache.Delete(req)
}

func ExampleHTTPCache_Keys() {
	httpCache := exampleHTTPCache()
	req := cache.CachedRequest{Method: "GET", URL: "https://example.com/data"}
	httpCache.Put(req, cache.CachedResponse{Status: 200, StatusText: "OK"}, []byte("body"))
	httpCache.Keys()
}

func ExampleCache_Clear() {
	c := exampleCache()
	c.Set("agent/profile", "codex")
	c.Clear()
}

func ExampleCache_Age() {
	c := exampleCache()
	c.Set("agent/profile", "codex")
	c.Age("agent/profile")
}

func ExampleGitHubReposKey() {
	cache.GitHubReposKey("acme")
}

func ExampleGitHubRepoKey() {
	cache.GitHubRepoKey("acme", "widgets")
}
