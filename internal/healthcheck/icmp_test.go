package healthcheck

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestICMPProbe_Localhost(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(5 * time.Second)

	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), 0, "")
	if err != nil {
		t.Errorf("expected success pinging localhost, got error: %v", err)
	}
}

func TestICMPProbe_LocalhostIPv6(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(5 * time.Second)

	err := probe.Check(context.Background(), net.ParseIP("::1"), 0, "")
	if err != nil {
		t.Errorf("expected success pinging localhost IPv6, got error: %v", err)
	}
}

func TestICMPProbe_Unreachable(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(500 * time.Millisecond)

	start := time.Now()
	err := probe.Check(context.Background(), net.ParseIP("10.255.255.1"), 0, "")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error for unreachable host, got nil")
	}

	// Should timeout around the probe timeout
	if elapsed > 1*time.Second {
		t.Errorf("expected timeout around 500ms, took %v", elapsed)
	}
}

func TestICMPProbe_ContextCancellation(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	probe := NewICMPProbe(5 * time.Second)

	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), 0, "")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestICMPProbe_ContextDeadlineShorterThanTimeout(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	// Context deadline is shorter than probe timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	probe := NewICMPProbe(5 * time.Second)

	start := time.Now()
	err := probe.Check(ctx, net.ParseIP("10.255.255.1"), 0, "")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Should respect context deadline, not probe timeout
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected to respect context deadline (~100ms), took %v", elapsed)
	}
}

func TestICMPProbe_PortAndPathIgnored(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(5 * time.Second)

	// Port and path should be ignored - test that different values don't affect behavior
	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), 8080, "/health")
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}

	err = probe.Check(context.Background(), net.ParseIP("127.0.0.1"), 0, "")
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
}

func TestICMPProbe_ErrorContainsICMP(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(100 * time.Millisecond)

	err := probe.Check(context.Background(), net.ParseIP("10.255.255.1"), 0, "")
	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "icmp") {
		t.Errorf("expected error to contain 'icmp', got: %v", err)
	}
}
