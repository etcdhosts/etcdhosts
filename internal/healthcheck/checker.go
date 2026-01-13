package healthcheck

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/etcdhosts/etcdhosts/v2/internal/hosts"
)

// UnhealthyPolicy defines how to handle unhealthy backends.
type UnhealthyPolicy string

const (
	// PolicyReturnAll returns all IPs regardless of health status.
	PolicyReturnAll UnhealthyPolicy = "return_all"
	// PolicyReturnEmpty returns an empty list when all IPs are unhealthy.
	PolicyReturnEmpty UnhealthyPolicy = "return_empty"
	// PolicyFallthrough lets the next plugin handle the request when all IPs are unhealthy.
	PolicyFallthrough UnhealthyPolicy = "fallthrough"
)

// Default configuration values
const (
	DefaultInterval           = 10 * time.Second
	DefaultTimeout            = 3 * time.Second
	DefaultMaxConcurrent      = 10
	DefaultFailuresBeforeDown = 3
	DefaultSuccessBeforeUp    = 1
)

// Config holds the configuration for the health checker.
type Config struct {
	Interval           time.Duration   // Check interval, default 10s
	Timeout            time.Duration   // Check timeout, default 3s
	MaxConcurrent      int             // Max concurrent checks, default 10
	CacheTTL           time.Duration   // Cache TTL, default interval*2
	FailuresBeforeDown int             // Failures needed to mark unhealthy, default 3
	SuccessBeforeUp    int             // Successes needed to mark healthy, default 1
	UnhealthyPolicy    UnhealthyPolicy // Policy for unhealthy backends
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		Interval:           DefaultInterval,
		Timeout:            DefaultTimeout,
		MaxConcurrent:      DefaultMaxConcurrent,
		CacheTTL:           DefaultInterval * 2,
		FailuresBeforeDown: DefaultFailuresBeforeDown,
		SuccessBeforeUp:    DefaultSuccessBeforeUp,
		UnhealthyPolicy:    PolicyReturnAll,
	}
}

// Target represents a health check target.
type Target struct {
	Hostname string
	IP       net.IP
	Health   *hosts.Health
}

// CacheKey returns the cache key for this target.
func (t Target) CacheKey() string {
	return fmt.Sprintf("%s:%s", t.Hostname, t.IP.String())
}

// Checker manages health checks for multiple targets.
type Checker struct {
	cfg     *Config
	cache   *Cache
	targets []Target
	probes  map[hosts.CheckType]Probe

	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewChecker creates a new Checker with the given configuration.
// Creates probes for all supported check types.
func NewChecker(cfg *Config) *Checker {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Apply defaults for zero values
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = DefaultMaxConcurrent
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = cfg.Interval * 2
	}
	if cfg.FailuresBeforeDown == 0 {
		cfg.FailuresBeforeDown = DefaultFailuresBeforeDown
	}
	if cfg.SuccessBeforeUp == 0 {
		cfg.SuccessBeforeUp = DefaultSuccessBeforeUp
	}

	// Create probes for each check type
	probes := map[hosts.CheckType]Probe{
		hosts.CheckTCP:   NewTCPProbe(cfg.Timeout),
		hosts.CheckHTTP:  NewHTTPProbe(cfg.Timeout, false),
		hosts.CheckHTTPS: NewHTTPProbe(cfg.Timeout, true),
		hosts.CheckICMP:  NewICMPProbe(cfg.Timeout),
	}

	return &Checker{
		cfg:    cfg,
		cache:  NewCache(cfg.CacheTTL),
		probes: probes,
	}
}

// UpdateTargets updates the list of targets to check.
// Extracts targets from records that have Health != nil.
func (c *Checker) UpdateTargets(records []hosts.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targets := make([]Target, 0)
	for _, r := range records {
		if r.Health != nil {
			targets = append(targets, Target{
				Hostname: r.Hostname,
				IP:       r.IP,
				Health:   r.Health,
			})
		}
	}
	c.targets = targets
}

// Start begins periodic health checking.
// Blocks until Stop is called or context is cancelled.
func (c *Checker) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.stopOnce = sync.Once{}
	c.mu.Unlock()

	// Run initial check immediately
	c.checkAll(ctx)

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.stop()
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkAll(ctx)
		}
	}
}

// Stop stops the periodic health checking.
func (c *Checker) Stop() {
	c.stop()
}

func (c *Checker) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}

	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	c.running = false
}

// IsHealthy returns whether the given hostname/IP combination is healthy.
// Returns true if:
// - The target is not being health checked (no Health config)
// - The target is in the cache and marked healthy
// - The target is not in the cache (optimistic default)
func (c *Checker) IsHealthy(hostname string, ip net.IP) bool {
	key := fmt.Sprintf("%s:%s", hostname, ip.String())
	return c.cache.IsHealthy(key)
}

// GetPolicy returns the configured unhealthy policy.
func (c *Checker) GetPolicy() UnhealthyPolicy {
	return c.cfg.UnhealthyPolicy
}

// checkAll runs health checks on all targets concurrently.
// Uses a semaphore to limit concurrent checks.
func (c *Checker) checkAll(ctx context.Context) {
	c.mu.RLock()
	targets := make([]Target, len(c.targets))
	copy(targets, c.targets)
	c.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	// Create semaphore to limit concurrency
	sem := make(chan struct{}, c.cfg.MaxConcurrent)
	var wg sync.WaitGroup

	for _, target := range targets {
		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		default:
		}

		wg.Add(1)
		go func(t Target) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			c.checkOne(ctx, t)
		}(target)
	}

	wg.Wait()
}

// checkOne performs a single health check for the given target.
func (c *Checker) checkOne(ctx context.Context, target Target) {
	if target.Health == nil {
		return
	}

	probe, ok := c.probes[target.Health.Type]
	if !ok {
		// Unknown check type, mark as healthy
		c.cache.Update(target.CacheKey(), true, c.cfg.FailuresBeforeDown, c.cfg.SuccessBeforeUp)
		return
	}

	// Create timeout context for this check
	checkCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	// Perform the health check
	err := probe.Check(checkCtx, target.IP, target.Health.Port, target.Health.Path)
	healthy := err == nil

	// Update cache with result
	c.cache.Update(target.CacheKey(), healthy, c.cfg.FailuresBeforeDown, c.cfg.SuccessBeforeUp)
}

// Cache returns the underlying cache for inspection.
// Use with caution - modifications may cause race conditions.
func (c *Checker) Cache() *Cache {
	return c.cache
}

// IsRunning returns whether the checker is currently running.
func (c *Checker) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// TargetCount returns the number of targets being checked.
func (c *Checker) TargetCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.targets)
}
