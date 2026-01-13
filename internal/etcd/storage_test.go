package etcd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

// setupEmbeddedEtcd starts an embedded etcd server for testing.
// Returns the etcd instance and endpoint URL.
func setupEmbeddedEtcd(t *testing.T) (*embed.Etcd, string) {
	t.Helper()

	dir := t.TempDir()

	cfg := embed.NewConfig()
	cfg.Dir = dir

	// Use random ports - need to set both Listen and Advertise URLs
	lcurl, _ := url.Parse("http://127.0.0.1:0")
	lpurl, _ := url.Parse("http://127.0.0.1:0")
	cfg.ListenClientUrls = []url.URL{*lcurl}
	cfg.AdvertiseClientUrls = []url.URL{*lcurl}
	cfg.ListenPeerUrls = []url.URL{*lpurl}
	cfg.AdvertisePeerUrls = []url.URL{*lpurl}
	cfg.InitialCluster = fmt.Sprintf("default=%s", lpurl.String())
	cfg.LogLevel = "error" // Reduce noise in tests

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatalf("failed to start embedded etcd: %v", err)
	}

	// Wait for server to be ready
	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(30 * time.Second):
		e.Close()
		t.Fatal("etcd server took too long to start")
	}

	// Get the actual listening address
	endpoint := e.Clients[0].Addr().String()

	t.Cleanup(func() {
		e.Close()
	})

	return e, "http://" + endpoint
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestSingleKeyStorage_LoadAndWatch(t *testing.T) {
	if os.Getenv("ETCD_TEST") == "" {
		t.Skip("skipping etcd integration test; set ETCD_TEST=1 to run")
	}

	_, endpoint := setupEmbeddedEtcd(t)

	// Create client
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create etcd client: %v", err)
	}
	defer client.Close()

	// Overall test timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	key := "/etcdhosts/test"
	storage := newSingleKeyStorage(client, key)

	// Test 1: Load empty initially
	t.Run("LoadEmpty", func(t *testing.T) {
		data, rev, err := storage.Load(ctx)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if data != nil {
			t.Errorf("expected nil data for empty key, got %q", string(data))
		}
		if rev == 0 {
			t.Error("expected non-zero revision")
		}
	})

	// Test 2: Put data, then Load
	testData := "192.168.1.1 host1.example.com\n192.168.1.2 host2.example.com"
	t.Run("LoadAfterPut", func(t *testing.T) {
		_, err := client.Put(ctx, key, testData)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		data, rev, err := storage.Load(ctx)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if string(data) != testData {
			t.Errorf("expected %q, got %q", testData, string(data))
		}
		if rev == 0 {
			t.Error("expected non-zero revision")
		}
	})

	// Test 3: Watch receives updates
	t.Run("Watch", func(t *testing.T) {
		watchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		watchCh := storage.Watch(watchCtx)

		// Use channel to signal watch is ready
		errCh := make(chan error, 1)
		newData := "10.0.0.1 newhost.example.com"
		go func() {
			// Small delay to ensure watch is set up
			time.Sleep(100 * time.Millisecond)
			_, err := client.Put(ctx, key, newData)
			errCh <- err
		}()

		// Wait for watch event
		select {
		case event := <-watchCh:
			if event.Err != nil {
				t.Fatalf("watch error: %v", event.Err)
			}
			if string(event.Data) != newData {
				t.Errorf("expected %q, got %q", newData, string(event.Data))
			}
			if event.Version == 0 {
				t.Error("expected non-zero version")
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Put in goroutine failed: %v", err)
			}
			// Put succeeded but no watch event yet, wait a bit more
			select {
			case event := <-watchCh:
				if event.Err != nil {
					t.Fatalf("watch error: %v", event.Err)
				}
			case <-watchCtx.Done():
				t.Fatal("timeout waiting for watch event after Put")
			}
		case <-watchCtx.Done():
			t.Fatal("timeout waiting for watch event")
		}
	})

	// Test 4: Watch receives delete events
	t.Run("WatchDelete", func(t *testing.T) {
		watchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		watchCh := storage.Watch(watchCtx)

		// Delete the key
		errCh := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, err := client.Delete(ctx, key)
			errCh <- err
		}()

		// Wait for watch event
		select {
		case event := <-watchCh:
			if event.Err != nil {
				t.Fatalf("watch error: %v", event.Err)
			}
			if event.Data != nil {
				t.Errorf("expected nil data for delete event, got %q", string(event.Data))
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Delete in goroutine failed: %v", err)
			}
			select {
			case event := <-watchCh:
				if event.Err != nil {
					t.Fatalf("watch error: %v", event.Err)
				}
			case <-watchCtx.Done():
				t.Fatal("timeout waiting for watch event after Delete")
			}
		case <-watchCtx.Done():
			t.Fatal("timeout waiting for watch event")
		}
	})
}

func TestPerHostStorage_LoadAndWatch(t *testing.T) {
	if os.Getenv("ETCD_TEST") == "" {
		t.Skip("skipping etcd integration test; set ETCD_TEST=1 to run")
	}

	_, endpoint := setupEmbeddedEtcd(t)

	// Create client
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create etcd client: %v", err)
	}
	defer client.Close()

	// Overall test timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prefix := "/etcdhosts/hosts/"
	storage := newPerHostStorage(client, prefix)

	// Test 1: Load empty initially
	t.Run("LoadEmpty", func(t *testing.T) {
		data, rev, err := storage.Load(ctx)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if data != nil {
			t.Errorf("expected nil data for empty prefix, got %q", string(data))
		}
		if rev == 0 {
			t.Error("expected non-zero revision")
		}
	})

	// Test 2: Put multiple keys, Load should concatenate all values
	t.Run("LoadMultipleKeys", func(t *testing.T) {
		host1Data := "192.168.1.1 host1.example.com"
		host2Data := "192.168.1.2 host2.example.com"
		host3Data := "192.168.1.3 host3.example.com"

		_, err := client.Put(ctx, prefix+"host1", host1Data)
		if err != nil {
			t.Fatalf("Put host1 failed: %v", err)
		}
		_, err = client.Put(ctx, prefix+"host2", host2Data)
		if err != nil {
			t.Fatalf("Put host2 failed: %v", err)
		}
		_, err = client.Put(ctx, prefix+"host3", host3Data)
		if err != nil {
			t.Fatalf("Put host3 failed: %v", err)
		}

		data, rev, err := storage.Load(ctx)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		// Data should contain all hosts (order depends on key sorting)
		dataStr := string(data)
		if !contains(dataStr, host1Data) {
			t.Errorf("expected data to contain %q", host1Data)
		}
		if !contains(dataStr, host2Data) {
			t.Errorf("expected data to contain %q", host2Data)
		}
		if !contains(dataStr, host3Data) {
			t.Errorf("expected data to contain %q", host3Data)
		}
		if rev == 0 {
			t.Error("expected non-zero revision")
		}
	})

	// Test 3: Watch receives update events (signals reload needed)
	t.Run("Watch", func(t *testing.T) {
		watchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		watchCh := storage.Watch(watchCtx)

		// Add a new host
		host4Data := "192.168.1.4 host4.example.com"
		errCh := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, err := client.Put(ctx, prefix+"host4", host4Data)
			errCh <- err
		}()

		// Wait for watch event
		select {
		case event := <-watchCh:
			if event.Err != nil {
				t.Fatalf("watch error: %v", event.Err)
			}
			// In per-host mode, Data is nil (signals reload needed)
			if event.Version == 0 {
				t.Error("expected non-zero version")
			}
			// Verify data after reload
			data, _, err := storage.Load(ctx)
			if err != nil {
				t.Fatalf("Load after watch failed: %v", err)
			}
			if !contains(string(data), host4Data) {
				t.Errorf("expected data to contain new host after watch event")
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Put in goroutine failed: %v", err)
			}
			select {
			case event := <-watchCh:
				if event.Err != nil {
					t.Fatalf("watch error: %v", event.Err)
				}
			case <-watchCtx.Done():
				t.Fatal("timeout waiting for watch event after Put")
			}
		case <-watchCtx.Done():
			t.Fatal("timeout waiting for watch event")
		}
	})

	// Test 4: Watch on delete
	t.Run("WatchDelete", func(t *testing.T) {
		watchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		watchCh := storage.Watch(watchCtx)

		// Delete a host
		errCh := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, err := client.Delete(ctx, prefix+"host2")
			errCh <- err
		}()

		// Wait for watch event
		select {
		case event := <-watchCh:
			if event.Err != nil {
				t.Fatalf("watch error: %v", event.Err)
			}
			if event.Version == 0 {
				t.Error("expected non-zero version")
			}
			// Verify data after reload - host2 should be gone
			data, _, err := storage.Load(ctx)
			if err != nil {
				t.Fatalf("Load after watch failed: %v", err)
			}
			if contains(string(data), "host2.example.com") {
				t.Errorf("expected host2 to be removed after delete")
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Delete in goroutine failed: %v", err)
			}
			select {
			case event := <-watchCh:
				if event.Err != nil {
					t.Fatalf("watch error: %v", event.Err)
				}
			case <-watchCtx.Done():
				t.Fatal("timeout waiting for watch event after Delete")
			}
		case <-watchCtx.Done():
			t.Fatal("timeout waiting for watch event")
		}
	})
}
