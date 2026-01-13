package healthcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPProbe implements the Probe interface using HTTP/HTTPS requests.
// It performs health checks by making HTTP GET requests to the specified endpoint.
type HTTPProbe struct {
	client *http.Client
	https  bool
}

// NewHTTPProbe creates a new HTTPProbe with the specified timeout.
// If https is true, the probe will use HTTPS with InsecureSkipVerify enabled
// for health check purposes.
func NewHTTPProbe(timeout time.Duration, https bool) *HTTPProbe {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// Don't follow redirects - return the response from the original request
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &HTTPProbe{
		client: client,
		https:  https,
	}
}

// Check performs an HTTP GET request to the specified IP, port, and path.
// Returns nil if the response status code is 200-399 (success or redirect).
// Returns an error for 4xx/5xx status codes or connection failures.
func (p *HTTPProbe) Check(ctx context.Context, ip net.IP, port int, path string) error {
	// Validate port range
	if port < 1 || port > 65535 {
		return fmt.Errorf("http check failed: invalid port %d", port)
	}

	// Default path to "/" if empty
	if path == "" {
		path = "/"
	}

	// Build the URL with proper IPv6 handling
	scheme := "http"
	if p.https {
		scheme = "https"
	}

	var host string
	if ip.To4() == nil {
		// IPv6 address needs brackets
		host = fmt.Sprintf("[%s]:%d", ip.String(), port)
	} else {
		host = fmt.Sprintf("%s:%d", ip.String(), port)
	}

	url := fmt.Sprintf("%s://%s%s", scheme, host, path)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("http check failed: %w", err)
	}

	// Perform the request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Accept 2xx and 3xx status codes as healthy
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}

	return fmt.Errorf("http check failed: unhealthy status code %d", resp.StatusCode)
}
