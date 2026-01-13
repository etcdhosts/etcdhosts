package healthcheck

import (
	"context"
	"net"
)

// Probe defines the interface for health check probes.
// Different probe types (TCP, HTTP, etc.) implement this interface
// to check if a backend is healthy.
type Probe interface {
	// Check performs a health check against the specified target.
	// For TCP probes, path is ignored. For HTTP probes, path specifies the endpoint.
	// Returns nil if the check succeeds, or an error describing the failure.
	Check(ctx context.Context, ip net.IP, port int, path string) error
}
