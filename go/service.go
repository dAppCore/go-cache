// SPDX-License-Identifier: EUPL-1.2

// Service registration for the cache package. Exposes the Cache surface
// as a Core service with action handlers so consumers can wire cache
// operations through the same plumbing as every other core service.
//
// Usage example: `c, _ := core.New(core.WithName("cache", cache.NewService(cache.CacheConfig{BaseDir: "/var/lib/core/cache", TTL: time.Hour})))`

package cache

import (
	"context"
	"time"

	core "dappco.re/go"
	coreio "dappco.re/go/io"
)

// CacheConfig is the typed-options struct for the cache service. Empty
// values fall back to package defaults (`coreio.Local` medium, CWD-rooted
// `.core/cache` baseDir, `DefaultTTL` cacheTTL).
//
// Usage example: `cfg := cache.CacheConfig{BaseDir: "/var/lib/core/cache", TTL: time.Hour}`
type CacheConfig struct {
	// Medium is the storage backend. Nil → coreio.Local.
	Medium coreio.Medium
	// BaseDir is the root directory. Empty → CWD/.core/cache.
	BaseDir string
	// TTL is the default cache TTL. Zero → DefaultTTL (1 hour).
	TTL time.Duration
}

// Service is the registerable handle for the cache package — embeds
// *core.ServiceRuntime[CacheConfig] for typed options access and holds
// a live *Cache ready for direct method calls or action use.
//
// Usage example: `svc := core.MustServiceFor[*cache.Service](c, "cache"); _ = svc.Cache.Delete("key")`
type Service struct {
	*core.ServiceRuntime[CacheConfig]
	// Cache is the live *Cache the service was constructed with.
	// Usage example: `svc.Cache.Delete("key")`
	Cache         *Cache
	registrations core.Once
}

// NewService returns a factory that constructs the cache and produces a
// *Service ready for c.Service() registration. Use through core.WithName
// so the framework wires lifecycle (OnStartup registers actions).
//
// Usage example: `c, _ := core.New(core.WithName("cache", cache.NewService(cache.CacheConfig{BaseDir: "/var/lib/core/cache"})))`
func NewService(config CacheConfig) func(*core.Core) core.Result {
	return func(c *core.Core) core.Result {
		r := New(config.Medium, config.BaseDir, config.TTL)
		if !r.OK {
			return r
		}
		return core.Ok(&Service{
			ServiceRuntime: core.NewServiceRuntime(c, config),
			Cache:          r.Value.(*Cache),
		})
	}
}

// OnStartup registers the cache action handlers on the attached Core.
// Implements core.Startable. Idempotent via core.Once — multiple startups
// (e.g. test re-entry) won't double-register.
//
// Note: Get / Set / SetBinary / GetBinary stay direct method calls because
// they need a typed `dest any` argument that doesn't round-trip through
// Options cleanly. Other operations are exposed as actions.
//
// Usage example: `r := svc.OnStartup(ctx)`
func (s *Service) OnStartup(context.Context) core.Result {
	if s == nil {
		return core.Ok(nil)
	}
	s.registrations.Do(func() {
		c := s.Core()
		if c == nil {
			return
		}
		c.Action("cache.delete", s.handleDelete)
		c.Action("cache.delete_many", s.handleDeleteMany)
		c.Action("cache.path", s.handlePath)
		c.Action("cache.invalidate", s.handleInvalidate)
		c.Action("cache.clear_scope", s.handleClearScope)
	})
	return core.Ok(nil)
}

// OnShutdown is a no-op for the cache service — the Cache holds no
// long-lived handles requiring teardown. Implements core.Stoppable for
// shape parity with other services.
//
// Usage example: `r := svc.OnShutdown(ctx)`
func (s *Service) OnShutdown(context.Context) core.Result {
	return core.Ok(nil)
}

// handleDelete — `cache.delete` action handler. Reads opts.key.
//
//	r := c.Action("cache.delete").Run(ctx, core.NewOptions(
//	    core.Option{Key: "key", Value: "user.profile.42"},
//	))
func (s *Service) handleDelete(_ core.Context, opts core.Options) core.Result {
	if s == nil || s.Cache == nil {
		return core.Fail(core.E("cache.delete", "service not initialised", nil))
	}
	return s.Cache.Delete(opts.String("key"))
}

// handleDeleteMany — `cache.delete_many` action handler. Reads
// opts.keys (string slice). Removes every supplied key in one pass and
// returns a count of successful deletions in r.Value.
//
//	r := c.Action("cache.delete_many").Run(ctx, core.NewOptions(
//	    core.Option{Key: "keys", Value: []string{"a", "b", "c"}},
//	))
func (s *Service) handleDeleteMany(_ core.Context, opts core.Options) core.Result {
	if s == nil || s.Cache == nil {
		return core.Fail(core.E("cache.delete_many", "service not initialised", nil))
	}
	r := opts.Get("keys")
	if !r.OK {
		return core.Fail(core.E("cache.delete_many", "keys is required", nil))
	}
	keys, ok := r.Value.([]string)
	if !ok {
		return core.Fail(core.E("cache.delete_many", "keys must be []string", nil))
	}
	return s.Cache.DeleteMany(keys...)
}

// handlePath — `cache.path` action handler. Reads opts.key and returns
// the on-disk JSON path in r.Value.
//
//	r := c.Action("cache.path").Run(ctx, core.NewOptions(
//	    core.Option{Key: "key", Value: "user.profile.42"},
//	))
//	path, _ := r.Value.(string)
func (s *Service) handlePath(_ core.Context, opts core.Options) core.Result {
	if s == nil || s.Cache == nil {
		return core.Fail(core.E("cache.path", "service not initialised", nil))
	}
	return s.Cache.Path(opts.String("key"))
}

// handleInvalidate — `cache.invalidate` action handler. Reads
// opts.trigger and runs every InvalidateFunc registered for that trigger.
//
//	r := c.Action("cache.invalidate").Run(ctx, core.NewOptions(
//	    core.Option{Key: "trigger", Value: "user.updated"},
//	))
func (s *Service) handleInvalidate(_ core.Context, opts core.Options) core.Result {
	if s == nil || s.Cache == nil {
		return core.Fail(core.E("cache.invalidate", "service not initialised", nil))
	}
	return s.Cache.Invalidate(opts.String("trigger"))
}

// handleClearScope — `cache.clear_scope` action handler. Reads
// opts.origin and removes every entry under that scope.
//
//	r := c.Action("cache.clear_scope").Run(ctx, core.NewOptions(
//	    core.Option{Key: "origin", Value: "users"},
//	))
func (s *Service) handleClearScope(_ core.Context, opts core.Options) core.Result {
	if s == nil || s.Cache == nil {
		return core.Fail(core.E("cache.clear_scope", "service not initialised", nil))
	}
	return s.Cache.ClearScope(opts.String("origin"))
}
