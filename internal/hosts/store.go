package hosts

import (
	"net"
	"strings"
	"sync"
)

// Store holds parsed hosts records with fast lookup maps.
type Store struct {
	mu      sync.RWMutex
	name4   map[string][]Entry  // hostname -> IPv4 entries
	name6   map[string][]Entry  // hostname -> IPv6 entries
	addr    map[string][]string // IP -> hostnames (reverse lookup)
	records []Record            // all records for iteration
}

// NewStore creates a new empty Store.
func NewStore() *Store {
	return &Store{
		name4: make(map[string][]Entry),
		name6: make(map[string][]Entry),
		addr:  make(map[string][]string),
	}
}

// Update replaces all records in the store.
func (s *Store) Update(records []Record) {
	name4 := make(map[string][]Entry)
	name6 := make(map[string][]Entry)
	addr := make(map[string][]string)

	for _, r := range records {
		hostname := strings.ToLower(r.Hostname)
		entry := Entry{
			IP:     r.IP,
			TTL:    r.TTL,
			Weight: r.Weight,
			Health: r.Health,
		}

		if r.IP.To4() != nil {
			name4[hostname] = append(name4[hostname], entry)
		} else {
			name6[hostname] = append(name6[hostname], entry)
		}

		// Reverse mapping
		ipStr := r.IP.String()
		addr[ipStr] = appendUnique(addr[ipStr], hostname)
	}

	s.mu.Lock()
	s.name4 = name4
	s.name6 = name6
	s.addr = addr
	s.records = records
	s.mu.Unlock()
}

// LookupV4 returns IPv4 addresses for hostname.
func (s *Store) LookupV4(hostname string) []net.IP {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.name4[strings.ToLower(hostname)]
	if len(entries) == 0 {
		return nil
	}
	ips := make([]net.IP, len(entries))
	for i, e := range entries {
		ips[i] = e.IP
	}
	return ips
}

// LookupV6 returns IPv6 addresses for hostname.
func (s *Store) LookupV6(hostname string) []net.IP {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.name6[strings.ToLower(hostname)]
	if len(entries) == 0 {
		return nil
	}
	ips := make([]net.IP, len(entries))
	for i, e := range entries {
		ips[i] = e.IP
	}
	return ips
}

// LookupAddr returns hostnames for an IP address (reverse lookup).
func (s *Store) LookupAddr(addr string) []string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	hosts := s.addr[ip.String()]
	if len(hosts) == 0 {
		return nil
	}

	result := make([]string, len(hosts))
	copy(result, hosts)
	return result
}

// Len returns total number of records.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// LookupV4WithWildcard returns IPv4 entries for hostname, including wildcard matches.
func (s *Store) LookupV4WithWildcard(hostname string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lookupWithWildcard(s.name4, hostname)
}

// LookupV6WithWildcard returns IPv6 entries for hostname, including wildcard matches.
func (s *Store) LookupV6WithWildcard(hostname string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lookupWithWildcard(s.name6, hostname)
}

func (s *Store) lookupWithWildcard(m map[string][]Entry, hostname string) []Entry {
	hostname = strings.ToLower(hostname)

	// Try exact match first
	if entries, ok := m[hostname]; ok {
		result := make([]Entry, len(entries))
		copy(result, entries)
		return result
	}

	// Collect all matching wildcard patterns
	var patterns []string
	for pattern := range m {
		if isWildcard(pattern) && wildcardMatch(pattern, hostname) {
			patterns = append(patterns, pattern)
		}
	}

	if len(patterns) == 0 {
		return nil
	}

	// Select best match
	best := selectBestMatch(patterns, hostname)
	if best == "" {
		return nil
	}

	entries := m[best]
	result := make([]Entry, len(entries))
	copy(result, entries)
	return result
}

// appendUnique appends s to slice if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}
