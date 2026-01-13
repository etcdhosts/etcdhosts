package healthcheck

import (
	"sync"
	"time"
)

// CacheEntry stores the health status for a single target.
type CacheEntry struct {
	Healthy   bool      // Current health status
	LastCheck time.Time // Time of last health check
	Failures  int       // Consecutive failure count
	Successes int       // Consecutive success count
}

// Cache stores health check results with TTL-based expiration.
// It is safe for concurrent access.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry // key: "hostname:ip"
	ttl     time.Duration
}

// NewCache creates a new Cache with the specified TTL.
// Entries older than TTL are considered stale but still return their last known value.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves the health status for the given key.
// Returns (healthy, true) if found, (true, false) if not found.
// Unknown entries are assumed healthy (optimistic default).
func (c *Cache) Get(key string) (healthy bool, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		// Not found, return healthy as default
		return true, false
	}

	return entry.Healthy, true
}

// Update records a health check result for the given key.
// The health state uses hysteresis: requires failuresBeforeDown consecutive failures
// to mark as unhealthy, and successBeforeUp consecutive successes to mark as healthy.
func (c *Cache) Update(key string, healthy bool, failuresBeforeDown, successBeforeUp int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		// New entry, start in healthy state
		entry = &CacheEntry{
			Healthy: true,
		}
		c.entries[key] = entry
	}

	entry.LastCheck = time.Now()

	if healthy {
		entry.Failures = 0
		entry.Successes++

		// Only mark healthy if we have enough consecutive successes
		if entry.Successes >= successBeforeUp {
			entry.Healthy = true
		}
	} else {
		entry.Successes = 0
		entry.Failures++

		// Only mark unhealthy if we have enough consecutive failures
		if entry.Failures >= failuresBeforeDown {
			entry.Healthy = false
		}
	}
}

// IsHealthy returns the health status for the given key.
// Returns true if the entry is not found (optimistic default).
func (c *Cache) IsHealthy(key string) bool {
	healthy, _ := c.Get(key)
	return healthy
}

// Keys returns all keys currently in the cache.
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	return keys
}

// IsStale checks if the entry for the given key has exceeded the TTL.
// Returns true if the entry is stale or not found.
func (c *Cache) IsStale(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return true
	}

	return time.Since(entry.LastCheck) > c.ttl
}

// GetEntry returns the full cache entry for the given key.
// Returns nil if not found.
func (c *Cache) GetEntry(key string) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil
	}

	// Return a copy to avoid race conditions
	return &CacheEntry{
		Healthy:   entry.Healthy,
		LastCheck: entry.LastCheck,
		Failures:  entry.Failures,
		Successes: entry.Successes,
	}
}

// Delete removes an entry from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

// Len returns the number of entries in the cache.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}
