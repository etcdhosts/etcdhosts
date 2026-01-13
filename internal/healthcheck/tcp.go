package healthcheck

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPProbe implements the Probe interface using TCP connection checks.
// It attempts to establish a TCP connection to verify that the target is reachable.
type TCPProbe struct {
	timeout time.Duration
}

// NewTCPProbe creates a new TCPProbe with the specified timeout.
// The timeout is used as the maximum duration for establishing a connection.
func NewTCPProbe(timeout time.Duration) *TCPProbe {
	return &TCPProbe{
		timeout: timeout,
	}
}

// Check attempts to establish a TCP connection to the specified IP and port.
// The path parameter is ignored for TCP probes.
// Returns nil if the connection succeeds, or an error if it fails.
func (p *TCPProbe) Check(ctx context.Context, ip net.IP, port int, _ string) error {
	// Validate port range
	if port < 1 || port > 65535 {
		return fmt.Errorf("tcp connect failed: invalid port %d", port)
	}

	// Determine if IPv6 (store result to avoid redundant check)
	isIPv6 := ip.To4() == nil

	// Determine the network type based on IP version
	network := "tcp4"
	if isIPv6 {
		network = "tcp6"
	}

	// Build the address string (IPv6 addresses need brackets)
	var addr string
	if isIPv6 {
		addr = fmt.Sprintf("[%s]:%d", ip.String(), port)
	} else {
		addr = fmt.Sprintf("%s:%d", ip.String(), port)
	}

	// Create dialer with timeout
	dialer := &net.Dialer{
		Timeout: p.timeout,
	}

	// DialContext respects both the context deadline and the dialer timeout,
	// using whichever is shorter
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return fmt.Errorf("tcp connect failed: %w", err)
	}

	// Successfully connected, close the connection
	_ = conn.Close()
	return nil
}
