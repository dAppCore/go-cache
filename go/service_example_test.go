package cache_test

import (
	"context"

	core "dappco.re/go"
	"dappco.re/go/cache"
)

// ExampleNewService constructs the cache service factory through
// `NewService` for go-cache Core service registration. The factory
// produces a *cache.Service ready for c.Service() — OnStartup wires
// the cache.* action handlers, OnShutdown is a no-op.
//
// Usage example: `c.Service("cache", cache.NewService(cache.CacheConfig{BaseDir: "/var/lib/core/cache"}))`
func ExampleNewService() {
	factory := cache.NewService(cache.CacheConfig{})
	core.Println(factory != nil)
	// Output: true
}

// ExampleService_OnStartup registers the cache.* action handlers on the
// attached Core through `Service.OnStartup` for go-cache Core service
// registration. Idempotent — multiple startups won't double-register.
//
// Usage example: `r := svc.OnStartup(ctx)`
func ExampleService_OnStartup() {
	c := core.New()
	r := cache.NewService(cache.CacheConfig{})(c)
	if !r.OK {
		core.Println("startup-init-failed")
		return
	}
	svc := r.Value.(*cache.Service)
	startup := svc.OnStartup(context.Background())
	core.Println(startup.OK)
	// Output: true
}

// ExampleService_OnShutdown drains the service through
// `Service.OnShutdown` for go-cache Core service registration. The cache
// holds no long-lived handles requiring teardown — Shutdown is a no-op
// returning Ok for shape parity with other services.
//
// Usage example: `r := svc.OnShutdown(ctx)`
func ExampleService_OnShutdown() {
	c := core.New()
	r := cache.NewService(cache.CacheConfig{})(c)
	if !r.OK {
		core.Println("startup-init-failed")
		return
	}
	svc := r.Value.(*cache.Service)
	shutdown := svc.OnShutdown(context.Background())
	core.Println(shutdown.OK)
	// Output: true
}
