package hosts

import (
	"net"
	"testing"
)

func TestStore_BasicLookup(t *testing.T) {
	store := NewStore()

	records := []Record{
		{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
		{Hostname: "example.com.", IP: net.ParseIP("192.168.1.2"), Weight: 1},
		{Hostname: "ipv6.example.com.", IP: net.ParseIP("2001:db8::1"), Weight: 1},
	}

	store.Update(records)

	t.Run("lookup ipv4", func(t *testing.T) {
		ips := store.LookupV4("example.com.")
		if len(ips) != 2 {
			t.Errorf("got %d IPs, want 2", len(ips))
		}
	})

	t.Run("lookup ipv6", func(t *testing.T) {
		ips := store.LookupV6("ipv6.example.com.")
		if len(ips) != 1 {
			t.Errorf("got %d IPs, want 1", len(ips))
		}
	})

	t.Run("lookup not found", func(t *testing.T) {
		ips := store.LookupV4("notfound.example.com.")
		if len(ips) != 0 {
			t.Errorf("got %d IPs, want 0", len(ips))
		}
	})

	t.Run("reverse lookup", func(t *testing.T) {
		hosts := store.LookupAddr("192.168.1.1")
		if len(hosts) != 1 || hosts[0] != "example.com." {
			t.Errorf("got %v, want [example.com.]", hosts)
		}
	})
}

func TestStore_CaseInsensitive(t *testing.T) {
	store := NewStore()
	store.Update([]Record{
		{Hostname: "Example.COM.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
	})

	ips := store.LookupV4("example.com.")
	if len(ips) != 1 {
		t.Errorf("case insensitive lookup failed, got %d IPs", len(ips))
	}
}

func TestStore_WildcardLookup(t *testing.T) {
	store := NewStore()
	store.Update([]Record{
		{Hostname: "*.example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
		{Hostname: "*.sub.example.com.", IP: net.ParseIP("192.168.1.2"), Weight: 1},
		{Hostname: "exact.example.com.", IP: net.ParseIP("192.168.1.3"), Weight: 1},
	})

	t.Run("exact match priority", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("exact.example.com.")
		if len(entries) != 1 || !entries[0].IP.Equal(net.ParseIP("192.168.1.3")) {
			t.Errorf("expected exact match IP 192.168.1.3, got %v", entries)
		}
	})

	t.Run("wildcard match", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("foo.example.com.")
		if len(entries) != 1 || !entries[0].IP.Equal(net.ParseIP("192.168.1.1")) {
			t.Errorf("expected wildcard match IP 192.168.1.1, got %v", entries)
		}
	})

	t.Run("longer wildcard priority", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("foo.sub.example.com.")
		if len(entries) != 1 || !entries[0].IP.Equal(net.ParseIP("192.168.1.2")) {
			t.Errorf("expected longer wildcard IP 192.168.1.2, got %v", entries)
		}
	})

	t.Run("no match", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("foo.other.com.")
		if len(entries) != 0 {
			t.Errorf("expected no match, got %v", entries)
		}
	})
}
