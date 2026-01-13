package healthcheck

import (
	"sync"
	"testing"
	"time"
)

func TestNewCache(t *testing.T) {
	ttl := 30 * time.Second
	c := NewCache(ttl)

	if c == nil {
		t.Fatal("NewCache returned nil")
	}
	if c.ttl != ttl {
		t.Errorf("expected ttl %v, got %v", ttl, c.ttl)
	}
	if len(c.entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(c.entries))
	}
}

func TestCache_Get_NotFound(t *testing.T) {
	c := NewCache(30 * time.Second)

	healthy, found := c.Get("nonexistent")
	if found {
		t.Error("expected found=false for nonexistent key")
	}
	if !healthy {
		t.Error("expected healthy=true for nonexistent key (optimistic default)")
	}
}

func TestCache_Get_Found(t *testing.T) {
	c := NewCache(30 * time.Second)

	// Add an entry
	c.Update("test:1.2.3.4", true, 3, 1)

	healthy, found := c.Get("test:1.2.3.4")
	if !found {
		t.Error("expected found=true")
	}
	if !healthy {
		t.Error("expected healthy=true")
	}
}

func TestCache_Update_HealthyToUnhealthy(t *testing.T) {
	c := NewCache(30 * time.Second)
	key := "test:1.2.3.4"
	failuresBeforeDown := 3
	successBeforeUp := 1

	// Start healthy
	c.Update(key, true, failuresBeforeDown, successBeforeUp)
	if !c.IsHealthy(key) {
		t.Error("expected healthy after success")
	}

	// First failure - should still be healthy
	c.Update(key, false, failuresBeforeDown, successBeforeUp)
	if !c.IsHealthy(key) {
		t.Error("expected still healthy after 1 failure")
	}

	// Second failure - should still be healthy
	c.Update(key, false, failuresBeforeDown, successBeforeUp)
	if !c.IsHealthy(key) {
		t.Error("expected still healthy after 2 failures")
	}

	// Third failure - should now be unhealthy
	c.Update(key, false, failuresBeforeDown, successBeforeUp)
	if c.IsHealthy(key) {
		t.Error("expected unhealthy after 3 failures")
	}
}

func TestCache_Update_UnhealthyToHealthy(t *testing.T) {
	c := NewCache(30 * time.Second)
	key := "test:1.2.3.4"
	failuresBeforeDown := 1
	successBeforeUp := 2

	// Make unhealthy first
	c.Update(key, false, failuresBeforeDown, successBeforeUp)
	if c.IsHealthy(key) {
		t.Error("expected unhealthy")
	}

	// First success - still unhealthy
	c.Update(key, true, failuresBeforeDown, successBeforeUp)
	if c.IsHealthy(key) {
		t.Error("expected still unhealthy after 1 success")
	}

	// Second success - should now be healthy
	c.Update(key, true, failuresBeforeDown, successBeforeUp)
	if !c.IsHealthy(key) {
		t.Error("expected healthy after 2 successes")
	}
}

func TestCache_Update_FailureResetsSuccessCount(t *testing.T) {
	c := NewCache(30 * time.Second)
	key := "test:1.2.3.4"

	// Make unhealthy
	c.Update(key, false, 1, 3)
	if c.IsHealthy(key) {
		t.Error("expected unhealthy")
	}

	// Two successes
	c.Update(key, true, 1, 3)
	c.Update(key, true, 1, 3)

	// One failure resets success count
	c.Update(key, false, 1, 3)

	entry := c.GetEntry(key)
	if entry.Successes != 0 {
		t.Errorf("expected successes=0 after failure, got %d", entry.Successes)
	}
	if entry.Failures != 1 {
		t.Errorf("expected failures=1, got %d", entry.Failures)
	}
}

func TestCache_Update_SuccessResetsFailureCount(t *testing.T) {
	c := NewCache(30 * time.Second)
	key := "test:1.2.3.4"

	// Two failures (not enough to mark unhealthy with threshold 3)
	c.Update(key, false, 3, 1)
	c.Update(key, false, 3, 1)

	// One success resets failure count
	c.Update(key, true, 3, 1)

	entry := c.GetEntry(key)
	if entry.Failures != 0 {
		t.Errorf("expected failures=0 after success, got %d", entry.Failures)
	}
	if entry.Successes != 1 {
		t.Errorf("expected successes=1, got %d", entry.Successes)
	}
}

func TestCache_IsHealthy(t *testing.T) {
	c := NewCache(30 * time.Second)

	// Unknown key returns healthy
	if !c.IsHealthy("unknown") {
		t.Error("expected healthy for unknown key")
	}

	// Known healthy key
	c.Update("healthy:1.2.3.4", true, 1, 1)
	if !c.IsHealthy("healthy:1.2.3.4") {
		t.Error("expected healthy")
	}

	// Known unhealthy key
	c.Update("unhealthy:1.2.3.4", false, 1, 1)
	if c.IsHealthy("unhealthy:1.2.3.4") {
		t.Error("expected unhealthy")
	}
}

func TestCache_Keys(t *testing.T) {
	c := NewCache(30 * time.Second)

	c.Update("a:1.1.1.1", true, 1, 1)
	c.Update("b:2.2.2.2", true, 1, 1)
	c.Update("c:3.3.3.3", true, 1, 1)

	keys := c.Keys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}

	for _, expected := range []string{"a:1.1.1.1", "b:2.2.2.2", "c:3.3.3.3"} {
		if !keySet[expected] {
			t.Errorf("missing expected key %s", expected)
		}
	}
}

func TestCache_IsStale(t *testing.T) {
	ttl := 100 * time.Millisecond
	c := NewCache(ttl)

	// Non-existent entry is stale
	if !c.IsStale("nonexistent") {
		t.Error("expected nonexistent entry to be stale")
	}

	// Fresh entry is not stale
	c.Update("test", true, 1, 1)
	if c.IsStale("test") {
		t.Error("expected fresh entry to not be stale")
	}

	// Wait for TTL to expire
	time.Sleep(ttl + 10*time.Millisecond)

	if !c.IsStale("test") {
		t.Error("expected expired entry to be stale")
	}
}

func TestCache_GetEntry(t *testing.T) {
	c := NewCache(30 * time.Second)

	// Non-existent entry returns nil
	if c.GetEntry("nonexistent") != nil {
		t.Error("expected nil for nonexistent entry")
	}

	// Existing entry returns copy
	c.Update("test", false, 1, 1)
	c.Update("test", false, 1, 1)

	entry := c.GetEntry("test")
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Failures != 2 {
		t.Errorf("expected failures=2, got %d", entry.Failures)
	}
	if entry.Healthy {
		t.Error("expected unhealthy")
	}
}

func TestCache_Delete(t *testing.T) {
	c := NewCache(30 * time.Second)

	c.Update("test", true, 1, 1)
	if c.Len() != 1 {
		t.Error("expected 1 entry")
	}

	c.Delete("test")
	if c.Len() != 0 {
		t.Error("expected 0 entries after delete")
	}

	// Verify entry is gone
	_, found := c.Get("test")
	if found {
		t.Error("expected entry to be deleted")
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(30 * time.Second)

	c.Update("a", true, 1, 1)
	c.Update("b", true, 1, 1)
	c.Update("c", true, 1, 1)

	if c.Len() != 3 {
		t.Error("expected 3 entries")
	}

	c.Clear()

	if c.Len() != 0 {
		t.Error("expected 0 entries after clear")
	}
}

func TestCache_Len(t *testing.T) {
	c := NewCache(30 * time.Second)

	if c.Len() != 0 {
		t.Error("expected 0 entries initially")
	}

	c.Update("a", true, 1, 1)
	if c.Len() != 1 {
		t.Error("expected 1 entry")
	}

	c.Update("b", true, 1, 1)
	if c.Len() != 2 {
		t.Error("expected 2 entries")
	}

	// Update existing key doesn't increase count
	c.Update("a", false, 1, 1)
	if c.Len() != 2 {
		t.Error("expected still 2 entries")
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c := NewCache(30 * time.Second)
	const numGoroutines = 100
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			key := "test"

			for j := 0; j < numOps; j++ {
				// Mix of read and write operations
				switch j % 4 {
				case 0:
					c.Update(key, j%2 == 0, 3, 1)
				case 1:
					c.Get(key)
				case 2:
					c.IsHealthy(key)
				case 3:
					c.Keys()
				}
			}
		}(i)
	}

	wg.Wait()
}
