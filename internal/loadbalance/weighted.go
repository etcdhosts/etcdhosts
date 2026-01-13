package loadbalance

import (
	"math/rand/v2"
	"net"
)

// Entry represents a single backend entry with its IP address, weight, and health status.
type Entry struct {
	IP      net.IP
	Weight  int
	Healthy bool
}

// WeightedBalancer implements weighted random load balancing.
// Higher weight entries have a proportionally higher chance of being selected first.
type WeightedBalancer struct{}

// NewWeightedBalancer creates a new WeightedBalancer instance.
func NewWeightedBalancer() *WeightedBalancer {
	return &WeightedBalancer{}
}

// Select returns a slice of IPs from healthy entries, ordered by weighted random selection.
// Higher weight entries are more likely to appear earlier in the result.
// Returns nil if no healthy entries are available.
func (b *WeightedBalancer) Select(entries []Entry) []net.IP {
	if len(entries) == 0 {
		return nil
	}

	// Filter healthy entries
	healthy := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Healthy {
			healthy = append(healthy, e)
		}
	}

	if len(healthy) == 0 {
		return nil
	}

	// Single entry: return directly without weight calculation
	if len(healthy) == 1 {
		return []net.IP{healthy[0].IP}
	}

	// Multiple entries: weighted random shuffle
	result := make([]net.IP, 0, len(healthy))
	remaining := make([]Entry, len(healthy))
	copy(remaining, healthy)

	for len(remaining) > 0 {
		selected := weightedSelect(remaining)
		result = append(result, remaining[selected].IP)

		// Remove selected entry
		remaining[selected] = remaining[len(remaining)-1]
		remaining = remaining[:len(remaining)-1]
	}

	return result
}

// weightedSelect performs a weighted random selection from the given entries.
// Returns the index of the selected entry.
// Entries with weight <= 0 are never selected unless all entries have weight <= 0.
// Precondition: entries must not be empty (caller guarantees this).
func weightedSelect(entries []Entry) int {
	if len(entries) == 1 {
		return 0
	}

	// Calculate total weight (only positive weights contribute)
	totalWeight := 0
	for _, e := range entries {
		if e.Weight > 0 {
			totalWeight += e.Weight
		}
	}

	// If all weights are zero or negative, select uniformly at random
	if totalWeight == 0 {
		return rand.IntN(len(entries))
	}

	// Weighted random selection
	r := rand.IntN(totalWeight)
	cumulative := 0
	for i, e := range entries {
		if e.Weight > 0 {
			cumulative += e.Weight
			if r < cumulative {
				return i
			}
		}
	}

	// Fallback (should not reach here if weights are correctly calculated)
	return len(entries) - 1
}
