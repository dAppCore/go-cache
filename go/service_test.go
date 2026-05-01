package cache

import (
	"context"

	core "dappco.re/go"
)

// --- AX-7 compliance triplets ---

func TestService_NewService_Good(t *core.T) {
	cfg := CacheConfig{BaseDir: t.TempDir()}
	factory := NewService(cfg)
	core.AssertNotNil(t, factory)
}

func TestService_NewService_Bad(t *core.T) {
	// NewService alone is a factory; resolution happens in c.Service().
	// Empty config falls back to package defaults.
	cfg := CacheConfig{}
	factory := NewService(cfg)
	core.AssertNotNil(t, factory)
}

func TestService_NewService_Ugly(t *core.T) {
	a := NewService(CacheConfig{BaseDir: t.TempDir()})
	b := NewService(CacheConfig{BaseDir: t.TempDir()})
	core.AssertNotNil(t, a)
	core.AssertNotNil(t, b)
}

// serviceForTest builds a *Service directly, mirroring the canonical
// pattern used in config/go/service_test.go: construct via factory then
// resolve through *Core.
func serviceForTest(t *core.T) *Service {
	t.Helper()
	c := core.New()
	r := NewService(CacheConfig{BaseDir: t.TempDir()})(c)
	core.RequireTrue(t, r.OK)
	return r.Value.(*Service)
}

func TestService_Service_OnStartup_Good(t *core.T) {
	svc := serviceForTest(t)
	startup := svc.OnStartup(context.Background())
	core.AssertTrue(t, startup.OK)
}

func TestService_Service_OnStartup_Bad(t *core.T) {
	var s *Service
	r := s.OnStartup(context.Background())
	core.AssertTrue(t, r.OK)
}

func TestService_Service_OnStartup_Ugly(t *core.T) {
	svc := serviceForTest(t)
	// Idempotent — second OnStartup is a no-op via core.Once.
	svc.OnStartup(context.Background())
	again := svc.OnStartup(context.Background())
	core.AssertTrue(t, again.OK)
}

func TestService_Service_OnShutdown_Good(t *core.T) {
	svc := serviceForTest(t)
	shutdown := svc.OnShutdown(context.Background())
	core.AssertTrue(t, shutdown.OK)
}

func TestService_Service_OnShutdown_Bad(t *core.T) {
	var s *Service
	r := s.OnShutdown(context.Background())
	core.AssertTrue(t, r.OK)
}

func TestService_Service_OnShutdown_Ugly(t *core.T) {
	svc := serviceForTest(t)
	// Multiple shutdowns return Ok cleanly.
	svc.OnShutdown(context.Background())
	again := svc.OnShutdown(context.Background())
	core.AssertTrue(t, again.OK)
}

// --- end AX-7 compliance triplets ---
