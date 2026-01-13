package healthcheck

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestTCPProbe_Success(t *testing.T) {
	// Start a local TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Accept connections in background
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}()

	addr := listener.Addr().(*net.TCPAddr)
	probe := NewTCPProbe(5 * time.Second)

	err = probe.Check(context.Background(), net.ParseIP("127.0.0.1"), addr.Port, "")
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
}

func TestTCPProbe_Failure(t *testing.T) {
	// Use a port that is definitely not listening
	// Port 0 is invalid for connecting, use an ephemeral port range
	// Start and immediately close a listener to get a definitely closed port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	_ = listener.Close()

	probe := NewTCPProbe(1 * time.Second)

	err = probe.Check(context.Background(), net.ParseIP("127.0.0.1"), addr.Port, "")
	if err == nil {
		t.Error("expected error for closed port, got nil")
	}
	if !strings.Contains(err.Error(), "tcp connect failed") {
		t.Errorf("expected error to contain 'tcp connect failed', got: %v", err)
	}
}

func TestTCPProbe_Timeout(t *testing.T) {
	// Use a non-routable IP address that will cause timeout
	// 10.255.255.1 is a non-routable address
	probe := NewTCPProbe(100 * time.Millisecond)

	start := time.Now()
	err := probe.Check(context.Background(), net.ParseIP("10.255.255.1"), 12345, "")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Should complete within reasonable time of timeout (with some buffer)
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected timeout around 100ms, took %v", elapsed)
	}
}

func TestTCPProbe_ContextCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	probe := NewTCPProbe(5 * time.Second)

	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), 12345, "")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestTCPProbe_ContextDeadlineShorterThanTimeout(t *testing.T) {
	// Context deadline is shorter than probe timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	probe := NewTCPProbe(5 * time.Second)

	start := time.Now()
	err := probe.Check(ctx, net.ParseIP("10.255.255.1"), 12345, "")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Should respect context deadline, not probe timeout
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected to respect context deadline (~50ms), took %v", elapsed)
	}
}

func TestTCPProbe_IPv6(t *testing.T) {
	// Start a local TCP server on IPv6
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 not available")
	}
	defer func() { _ = listener.Close() }()

	// Accept connections in background
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}()

	addr := listener.Addr().(*net.TCPAddr)
	probe := NewTCPProbe(5 * time.Second)

	err = probe.Check(context.Background(), net.ParseIP("::1"), addr.Port, "")
	if err != nil {
		t.Errorf("expected success for IPv6, got error: %v", err)
	}
}

func TestTCPProbe_InvalidPort(t *testing.T) {
	probe := NewTCPProbe(1 * time.Second)

	tests := []struct {
		name string
		port int
	}{
		{"port 0", 0},
		{"negative port", -1},
		{"port too high", 65536},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), tt.port, "")
			if err == nil {
				t.Errorf("expected error for invalid port %d, got nil", tt.port)
			}
			if !strings.Contains(err.Error(), "invalid port") {
				t.Errorf("expected error to mention 'invalid port', got: %v", err)
			}
		})
	}
}
