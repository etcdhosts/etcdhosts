package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPProbe_Success(t *testing.T) {
	// Create test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract port from server URL
	_, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse server address: %v", err)
	}
	var port int
	_, _ = net.LookupPort("tcp", portStr)
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	probe := NewHTTPProbe(5*time.Second, false)

	err = probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/")
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
}

func TestHTTPProbe_SuccessWithPath(t *testing.T) {
	// Create test server that checks the path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	probe := NewHTTPProbe(5*time.Second, false)

	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/health")
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
}

func TestHTTPProbe_DefaultPath(t *testing.T) {
	// Create test server that only responds to "/"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	probe := NewHTTPProbe(5*time.Second, false)

	// Empty path should default to "/"
	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "")
	if err != nil {
		t.Errorf("expected success with default path, got error: %v", err)
	}
}

func TestHTTPProbe_AcceptRedirectStatus(t *testing.T) {
	// Test that 3xx status codes are considered healthy
	tests := []struct {
		name   string
		status int
	}{
		{"301 Moved Permanently", http.StatusMovedPermanently},
		{"302 Found", http.StatusFound},
		{"307 Temporary Redirect", http.StatusTemporaryRedirect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
			var port int
			for i := 0; i < len(portStr); i++ {
				port = port*10 + int(portStr[i]-'0')
			}

			probe := NewHTTPProbe(5*time.Second, false)

			err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/")
			if err != nil {
				t.Errorf("expected status %d to be healthy, got error: %v", tt.status, err)
			}
		})
	}
}

func TestHTTPProbe_BadStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"400 Bad Request", http.StatusBadRequest},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
		{"502 Bad Gateway", http.StatusBadGateway},
		{"503 Service Unavailable", http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
			var port int
			for i := 0; i < len(portStr); i++ {
				port = port*10 + int(portStr[i]-'0')
			}

			probe := NewHTTPProbe(5*time.Second, false)

			err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/")
			if err == nil {
				t.Errorf("expected error for status %d, got nil", tt.status)
			}
			if !strings.Contains(err.Error(), "unhealthy status") {
				t.Errorf("expected error to contain 'unhealthy status', got: %v", err)
			}
		})
	}
}

func TestHTTPProbe_Timeout(t *testing.T) {
	// Create test server that sleeps longer than probe timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	probe := NewHTTPProbe(100*time.Millisecond, false)

	start := time.Now()
	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Should complete within reasonable time of timeout
	if elapsed > 300*time.Millisecond {
		t.Errorf("expected timeout around 100ms, took %v", elapsed)
	}
}

func TestHTTPProbe_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep long enough that context will definitely cancel first
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	// Create a context that times out quickly
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	probe := NewHTTPProbe(10*time.Second, false)

	start := time.Now()
	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), port, "/")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}

	// Should respect context deadline
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected to respect context deadline (~100ms), took %v", elapsed)
	}
}

func TestHTTPProbe_ConnectionRefused(t *testing.T) {
	// Get a port that is definitely not listening
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	_ = listener.Close()

	probe := NewHTTPProbe(1*time.Second, false)

	err = probe.Check(context.Background(), net.ParseIP("127.0.0.1"), addr.Port, "/")
	if err == nil {
		t.Error("expected error for closed port, got nil")
	}
}

func TestHTTPProbe_HTTPS(t *testing.T) {
	// Create test HTTPS server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	probe := NewHTTPProbe(5*time.Second, true)

	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/")
	if err != nil {
		t.Errorf("expected success for HTTPS, got error: %v", err)
	}
}

func TestHTTPProbe_NoFollowRedirect(t *testing.T) {
	redirectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount > 1 {
			t.Error("should not follow redirects")
		}
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	for i := 0; i < len(portStr); i++ {
		port = port*10 + int(portStr[i]-'0')
	}

	probe := NewHTTPProbe(5*time.Second, false)

	// Should not follow redirect, but 302 is still healthy
	err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), port, "/")
	if err != nil {
		t.Errorf("expected 302 to be healthy, got error: %v", err)
	}

	if redirectCount != 1 {
		t.Errorf("expected exactly 1 request (no redirect following), got %d", redirectCount)
	}
}

func TestHTTPProbe_InvalidPort(t *testing.T) {
	probe := NewHTTPProbe(1*time.Second, false)

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
			err := probe.Check(context.Background(), net.ParseIP("127.0.0.1"), tt.port, "/")
			if err == nil {
				t.Errorf("expected error for invalid port %d, got nil", tt.port)
			}
			if !strings.Contains(err.Error(), "invalid port") {
				t.Errorf("expected error to mention 'invalid port', got: %v", err)
			}
		})
	}
}
