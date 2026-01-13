package healthcheck

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/etcdhosts/etcdhosts/v2/internal/hosts"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Interval != DefaultInterval {
		t.Errorf("expected interval %v, got %v", DefaultInterval, cfg.Interval)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, cfg.Timeout)
	}
	if cfg.MaxConcurrent != DefaultMaxConcurrent {
		t.Errorf("expected maxConcurrent %d, got %d", DefaultMaxConcurrent, cfg.MaxConcurrent)
	}
	if cfg.CacheTTL != DefaultInterval*2 {
		t.Errorf("expected cacheTTL %v, got %v", DefaultInterval*2, cfg.CacheTTL)
	}
	if cfg.FailuresBeforeDown != DefaultFailuresBeforeDown {
		t.Errorf("expected failuresBeforeDown %d, got %d", DefaultFailuresBeforeDown, cfg.FailuresBeforeDown)
	}
	if cfg.SuccessBeforeUp != DefaultSuccessBeforeUp {
		t.Errorf("expected successBeforeUp %d, got %d", DefaultSuccessBeforeUp, cfg.SuccessBeforeUp)
	}
	if cfg.UnhealthyPolicy != PolicyReturnAll {
		t.Errorf("expected unhealthyPolicy %s, got %s", PolicyReturnAll, cfg.UnhealthyPolicy)
	}
}

func TestNewChecker_NilConfig(t *testing.T) {
	c := NewChecker(nil)

	if c == nil {
		t.Fatal("NewChecker returned nil")
	}
	if c.cfg == nil {
		t.Fatal("checker config is nil")
	}
	if c.cfg.Interval != DefaultInterval {
		t.Errorf("expected default interval, got %v", c.cfg.Interval)
	}
}

func TestNewChecker_WithConfig(t *testing.T) {
	cfg := &Config{
		Interval:           5 * time.Second,
		Timeout:            1 * time.Second,
		MaxConcurrent:      5,
		FailuresBeforeDown: 5,
		SuccessBeforeUp:    2,
		UnhealthyPolicy:    PolicyReturnEmpty,
	}

	c := NewChecker(cfg)

	if c.cfg.Interval != 5*time.Second {
		t.Errorf("expected interval 5s, got %v", c.cfg.Interval)
	}
	if c.cfg.Timeout != 1*time.Second {
		t.Errorf("expected timeout 1s, got %v", c.cfg.Timeout)
	}
	if c.cfg.MaxConcurrent != 5 {
		t.Errorf("expected maxConcurrent 5, got %d", c.cfg.MaxConcurrent)
	}
	if c.cfg.CacheTTL != 10*time.Second {
		t.Errorf("expected cacheTTL 10s (interval*2), got %v", c.cfg.CacheTTL)
	}
}

func TestNewChecker_CreatesProbes(t *testing.T) {
	c := NewChecker(nil)

	// Verify all probe types are created
	probeTypes := []hosts.CheckType{
		hosts.CheckTCP,
		hosts.CheckHTTP,
		hosts.CheckHTTPS,
		hosts.CheckICMP,
	}

	for _, pt := range probeTypes {
		if _, ok := c.probes[pt]; !ok {
			t.Errorf("missing probe for type %s", pt)
		}
	}
}

func TestTarget_CacheKey(t *testing.T) {
	target := Target{
		Hostname: "example.com",
		IP:       net.ParseIP("1.2.3.4"),
	}

	key := target.CacheKey()
	expected := "example.com:1.2.3.4"

	if key != expected {
		t.Errorf("expected key %s, got %s", expected, key)
	}
}

func TestTarget_CacheKey_IPv6(t *testing.T) {
	target := Target{
		Hostname: "example.com",
		IP:       net.ParseIP("2001:db8::1"),
	}

	key := target.CacheKey()
	expected := "example.com:2001:db8::1"

	if key != expected {
		t.Errorf("expected key %s, got %s", expected, key)
	}
}

func TestChecker_UpdateTargets(t *testing.T) {
	c := NewChecker(nil)

	records := []hosts.Record{
		{
			Hostname: "a.example.com",
			IP:       net.ParseIP("1.1.1.1"),
			Health:   &hosts.Health{Type: hosts.CheckTCP, Port: 80},
		},
		{
			Hostname: "b.example.com",
			IP:       net.ParseIP("2.2.2.2"),
			Health:   nil, // No health check
		},
		{
			Hostname: "c.example.com",
			IP:       net.ParseIP("3.3.3.3"),
			Health:   &hosts.Health{Type: hosts.CheckHTTP, Port: 8080, Path: "/health"},
		},
	}

	c.UpdateTargets(records)

	if c.TargetCount() != 2 {
		t.Errorf("expected 2 targets, got %d", c.TargetCount())
	}
}

func TestChecker_IsHealthy_UnknownTarget(t *testing.T) {
	c := NewChecker(nil)

	// Unknown targets should be considered healthy (optimistic default)
	if !c.IsHealthy("unknown.example.com", net.ParseIP("1.2.3.4")) {
		t.Error("expected unknown target to be healthy")
	}
}

func TestChecker_IsHealthy_KnownTarget(t *testing.T) {
	c := NewChecker(nil)

	// Manually update cache
	c.cache.Update("test.example.com:1.2.3.4", false, 1, 1)

	if c.IsHealthy("test.example.com", net.ParseIP("1.2.3.4")) {
		t.Error("expected target to be unhealthy")
	}
}

func TestChecker_GetPolicy(t *testing.T) {
	tests := []struct {
		policy   UnhealthyPolicy
		expected UnhealthyPolicy
	}{
		{PolicyReturnAll, PolicyReturnAll},
		{PolicyReturnEmpty, PolicyReturnEmpty},
		{PolicyFallthrough, PolicyFallthrough},
	}

	for _, tt := range tests {
		cfg := &Config{UnhealthyPolicy: tt.policy}
		c := NewChecker(cfg)

		if c.GetPolicy() != tt.expected {
			t.Errorf("expected policy %s, got %s", tt.expected, c.GetPolicy())
		}
	}
}

func TestChecker_IsRunning(t *testing.T) {
	c := NewChecker(&Config{Interval: 100 * time.Millisecond})

	if c.IsRunning() {
		t.Error("expected not running initially")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start in background
	go c.Start(ctx)

	// Wait a bit for start to take effect
	time.Sleep(50 * time.Millisecond)

	if !c.IsRunning() {
		t.Error("expected running after Start")
	}

	cancel()

	// Wait for stop to take effect
	time.Sleep(50 * time.Millisecond)

	if c.IsRunning() {
		t.Error("expected not running after cancel")
	}
}

func TestChecker_Stop(t *testing.T) {
	c := NewChecker(&Config{Interval: 100 * time.Millisecond})

	ctx := context.Background()

	// Start in background
	go c.Start(ctx)

	// Wait for start
	time.Sleep(50 * time.Millisecond)

	if !c.IsRunning() {
		t.Error("expected running")
	}

	c.Stop()

	// Wait for stop
	time.Sleep(50 * time.Millisecond)

	if c.IsRunning() {
		t.Error("expected not running after Stop")
	}
}

func TestChecker_Stop_Idempotent(t *testing.T) {
	c := NewChecker(&Config{Interval: 100 * time.Millisecond})

	ctx := context.Background()
	go c.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Multiple stops should not panic
	c.Stop()
	c.Stop()
	c.Stop()
}

func TestChecker_Start_Idempotent(t *testing.T) {
	c := NewChecker(&Config{Interval: 100 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First start
	go c.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Second start should be no-op
	go c.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	if !c.IsRunning() {
		t.Error("expected running")
	}
}

// mockProbe is a test probe that tracks calls
type mockProbe struct {
	checkCount int64
	healthy    bool
}

func (m *mockProbe) Check(ctx context.Context, ip net.IP, port int, path string) error {
	atomic.AddInt64(&m.checkCount, 1)
	if m.healthy {
		return nil
	}
	return context.DeadlineExceeded
}

func TestChecker_checkOne(t *testing.T) {
	c := NewChecker(&Config{
		FailuresBeforeDown: 1,
		SuccessBeforeUp:    1,
	})

	// Replace probe with mock
	mock := &mockProbe{healthy: true}
	c.probes[hosts.CheckTCP] = mock

	target := Target{
		Hostname: "test.example.com",
		IP:       net.ParseIP("1.2.3.4"),
		Health:   &hosts.Health{Type: hosts.CheckTCP, Port: 80},
	}

	ctx := context.Background()

	// Check healthy
	c.checkOne(ctx, target)
	if !c.IsHealthy(target.Hostname, target.IP) {
		t.Error("expected healthy after successful check")
	}

	// Check unhealthy
	mock.healthy = false
	c.checkOne(ctx, target)
	if c.IsHealthy(target.Hostname, target.IP) {
		t.Error("expected unhealthy after failed check")
	}
}

func TestChecker_checkAll_Concurrency(t *testing.T) {
	c := NewChecker(&Config{
		MaxConcurrent:      2,
		FailuresBeforeDown: 1,
		SuccessBeforeUp:    1,
	})

	// Replace probe with mock
	mock := &mockProbe{healthy: true}
	c.probes[hosts.CheckTCP] = mock

	// Create multiple targets
	records := make([]hosts.Record, 10)
	for i := 0; i < 10; i++ {
		records[i] = hosts.Record{
			Hostname: "test.example.com",
			IP:       net.ParseIP("1.2.3.4"),
			Health:   &hosts.Health{Type: hosts.CheckTCP, Port: 80},
		}
	}
	c.UpdateTargets(records)

	ctx := context.Background()
	c.checkAll(ctx)

	// Verify all targets were checked
	if atomic.LoadInt64(&mock.checkCount) != 10 {
		t.Errorf("expected 10 checks, got %d", mock.checkCount)
	}
}

func TestChecker_checkAll_ContextCancel(t *testing.T) {
	c := NewChecker(&Config{
		MaxConcurrent: 1,
		Timeout:       1 * time.Second,
	})

	// Create slow mock probe
	slowMock := &mockProbe{healthy: true}
	c.probes[hosts.CheckTCP] = slowMock

	// Create multiple targets
	records := make([]hosts.Record, 5)
	for i := 0; i < 5; i++ {
		records[i] = hosts.Record{
			Hostname: "test.example.com",
			IP:       net.ParseIP("1.2.3.4"),
			Health:   &hosts.Health{Type: hosts.CheckTCP, Port: 80},
		}
	}
	c.UpdateTargets(records)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	c.checkAll(ctx)

	// Should exit early due to cancelled context
	// Not all checks may have run
}

func TestChecker_checkOne_UnknownCheckType(t *testing.T) {
	c := NewChecker(&Config{
		FailuresBeforeDown: 1,
		SuccessBeforeUp:    1,
	})

	target := Target{
		Hostname: "test.example.com",
		IP:       net.ParseIP("1.2.3.4"),
		Health:   &hosts.Health{Type: "unknown", Port: 80},
	}

	ctx := context.Background()
	c.checkOne(ctx, target)

	// Unknown check type should be marked as healthy
	if !c.IsHealthy(target.Hostname, target.IP) {
		t.Error("expected healthy for unknown check type")
	}
}

func TestChecker_checkOne_NilHealth(t *testing.T) {
	c := NewChecker(nil)

	target := Target{
		Hostname: "test.example.com",
		IP:       net.ParseIP("1.2.3.4"),
		Health:   nil,
	}

	ctx := context.Background()

	// Should not panic
	c.checkOne(ctx, target)
}

func TestUnhealthyPolicy_Constants(t *testing.T) {
	// Verify policy constants
	if PolicyReturnAll != "return_all" {
		t.Errorf("unexpected PolicyReturnAll value: %s", PolicyReturnAll)
	}
	if PolicyReturnEmpty != "return_empty" {
		t.Errorf("unexpected PolicyReturnEmpty value: %s", PolicyReturnEmpty)
	}
	if PolicyFallthrough != "fallthrough" {
		t.Errorf("unexpected PolicyFallthrough value: %s", PolicyFallthrough)
	}
}

func TestChecker_Cache(t *testing.T) {
	c := NewChecker(nil)

	cache := c.Cache()
	if cache == nil {
		t.Error("expected non-nil cache")
	}

	// Verify it's the same cache
	c.cache.Update("test", true, 1, 1)
	if !cache.IsHealthy("test") {
		t.Error("expected cache to be shared")
	}
}
