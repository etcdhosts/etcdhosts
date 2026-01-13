package loadbalance

import (
	"net"
	"testing"
)

func TestWeightedBalancer_SingleEntry(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 1, Healthy: true},
	}

	result := balancer.Select(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 IP, got %d", len(result))
	}
	if !result[0].Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("expected 192.168.1.1, got %s", result[0])
	}
}

func TestWeightedBalancer_FilterUnhealthy(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 1, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: false},
		{IP: net.ParseIP("192.168.1.3"), Weight: 1, Healthy: true},
	}

	result := balancer.Select(entries)

	if len(result) != 2 {
		t.Fatalf("expected 2 IPs (unhealthy filtered), got %d", len(result))
	}

	// Check that unhealthy IP is not in result
	for _, ip := range result {
		if ip.Equal(net.ParseIP("192.168.1.2")) {
			t.Error("unhealthy IP 192.168.1.2 should be filtered out")
		}
	}

	// Check that healthy IPs are in result
	found1, found3 := false, false
	for _, ip := range result {
		if ip.Equal(net.ParseIP("192.168.1.1")) {
			found1 = true
		}
		if ip.Equal(net.ParseIP("192.168.1.3")) {
			found3 = true
		}
	}
	if !found1 || !found3 {
		t.Error("expected both healthy IPs in result")
	}
}

func TestWeightedBalancer_WeightDistribution(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 3, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: true},
	}

	// Run 10000 iterations and count how many times each IP appears first
	iterations := 10000
	firstCount := make(map[string]int)

	for i := 0; i < iterations; i++ {
		result := balancer.Select(entries)
		if len(result) > 0 {
			firstCount[result[0].String()]++
		}
	}

	// Weight 3:1 should result in approximately 75%:25% distribution
	ip1Count := firstCount[net.ParseIP("192.168.1.1").String()]
	ip2Count := firstCount[net.ParseIP("192.168.1.2").String()]

	expectedRatio := 0.75
	actualRatio := float64(ip1Count) / float64(iterations)
	tolerance := 0.05

	if actualRatio < expectedRatio-tolerance || actualRatio > expectedRatio+tolerance {
		t.Errorf("weight distribution outside tolerance: expected ~%.2f, got %.4f (ip1: %d, ip2: %d)",
			expectedRatio, actualRatio, ip1Count, ip2Count)
	}
}

func TestWeightedBalancer_AllUnhealthy(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 1, Healthy: false},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: false},
	}

	result := balancer.Select(entries)

	if result != nil {
		t.Errorf("expected nil when all unhealthy, got %v", result)
	}
}

func TestWeightedBalancer_EmptyEntries(t *testing.T) {
	balancer := NewWeightedBalancer()

	result := balancer.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil entries, got %v", result)
	}

	result = balancer.Select([]Entry{})
	if result != nil {
		t.Errorf("expected nil for empty entries, got %v", result)
	}
}

func TestWeightedBalancer_ZeroWeight(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 0, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: true},
	}

	// Run multiple times to ensure zero-weight entry never appears first
	for i := 0; i < 100; i++ {
		result := balancer.Select(entries)
		if len(result) > 0 && result[0].Equal(net.ParseIP("192.168.1.1")) {
			t.Error("zero-weight entry should not appear first when other entries have weight")
			break
		}
	}
}

func TestWeightedBalancer_NegativeWeight(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: -5, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: true},
	}

	// Run multiple times to ensure negative-weight entry never appears first
	for i := 0; i < 100; i++ {
		result := balancer.Select(entries)
		if len(result) > 0 && result[0].Equal(net.ParseIP("192.168.1.1")) {
			t.Error("negative-weight entry should not appear first when other entries have positive weight")
			break
		}
	}
}

func TestWeightedBalancer_AllZeroWeight(t *testing.T) {
	balancer := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 0, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 0, Healthy: true},
	}

	// When all weights are zero, should fall back to uniform random selection
	// Just verify it returns both IPs without error
	result := balancer.Select(entries)
	if len(result) != 2 {
		t.Errorf("expected 2 IPs, got %d", len(result))
	}

	// Verify both IPs are in result
	found1, found2 := false, false
	for _, ip := range result {
		if ip.Equal(net.ParseIP("192.168.1.1")) {
			found1 = true
		}
		if ip.Equal(net.ParseIP("192.168.1.2")) {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Error("expected both IPs in result when using uniform random fallback")
	}
}
