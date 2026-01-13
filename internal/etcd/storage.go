package etcd

import (
	"bytes"
	"context"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// StorageMode defines how hosts data is stored in etcd.
type StorageMode int

const (
	// ModeSingle stores all hosts in a single key.
	ModeSingle StorageMode = iota
	// ModePerHost stores each hostname as a separate key.
	ModePerHost
)

// Storage defines the interface for loading and watching hosts data from etcd.
type Storage interface {
	// Load retrieves the current hosts data from etcd.
	// Returns the data, etcd revision, and any error.
	Load(ctx context.Context) ([]byte, int64, error)

	// Watch returns a channel that receives events when data changes.
	// The channel is closed when the context is cancelled or on unrecoverable error.
	Watch(ctx context.Context) <-chan WatchEvent

	// Close releases any resources held by the storage.
	Close() error
}

// WatchEvent represents a change notification from etcd.
type WatchEvent struct {
	Data    []byte // New data (nil for delete events)
	Version int64  // etcd revision
	Err     error  // Error if any (channel closed after error)
}

// singleKeyStorage implements Storage for single-key mode.
type singleKeyStorage struct {
	client *clientv3.Client
	key    string
}

// newSingleKeyStorage creates a storage that stores all hosts in one key.
func newSingleKeyStorage(client *clientv3.Client, key string) *singleKeyStorage {
	return &singleKeyStorage{
		client: client,
		key:    key,
	}
}

// Load retrieves hosts data from the single key.
func (s *singleKeyStorage) Load(ctx context.Context) ([]byte, int64, error) {
	resp, err := s.client.Get(ctx, s.key)
	if err != nil {
		return nil, 0, err
	}

	if len(resp.Kvs) == 0 {
		return nil, resp.Header.Revision, nil
	}

	return resp.Kvs[0].Value, resp.Header.Revision, nil
}

// Watch watches for changes to the single key.
func (s *singleKeyStorage) Watch(ctx context.Context) <-chan WatchEvent {
	ch := make(chan WatchEvent, 1)

	go func() {
		defer close(ch)

		// Use WithRequireLeader to ensure we're connected to the leader
		watchCtx := clientv3.WithRequireLeader(ctx)
		watcher := s.client.Watch(watchCtx, s.key)

		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-watcher:
				if !ok {
					// Watcher channel closed
					return
				}

				if resp.Err() != nil {
					ch <- WatchEvent{Err: resp.Err()}
					return
				}

				for _, ev := range resp.Events {
					event := WatchEvent{
						Version: resp.Header.Revision,
					}
					if ev.Type == clientv3.EventTypeDelete {
						event.Data = nil
					} else {
						event.Data = ev.Kv.Value
					}
					select {
					case ch <- event:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}

// Close is a no-op for singleKeyStorage as the client is managed externally.
func (s *singleKeyStorage) Close() error {
	return nil
}

// perHostStorage implements Storage for per-host mode.
type perHostStorage struct {
	client *clientv3.Client
	prefix string
}

// newPerHostStorage creates a storage that stores each host as a separate key.
func newPerHostStorage(client *clientv3.Client, prefix string) *perHostStorage {
	return &perHostStorage{
		client: client,
		prefix: prefix,
	}
}

// Load retrieves all hosts data by concatenating values under the prefix.
func (s *perHostStorage) Load(ctx context.Context) ([]byte, int64, error) {
	resp, err := s.client.Get(ctx, s.prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, 0, err
	}

	if len(resp.Kvs) == 0 {
		return nil, resp.Header.Revision, nil
	}

	// Concatenate all values with newlines
	var buf bytes.Buffer
	for i, kv := range resp.Kvs {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.Write(kv.Value)
	}

	return buf.Bytes(), resp.Header.Revision, nil
}

// Watch watches for any changes under the prefix.
// Any change triggers a full reload notification (data is nil, caller should call Load).
func (s *perHostStorage) Watch(ctx context.Context) <-chan WatchEvent {
	ch := make(chan WatchEvent, 1)

	go func() {
		defer close(ch)

		// Use WithRequireLeader to ensure we're connected to the leader
		watchCtx := clientv3.WithRequireLeader(ctx)
		watcher := s.client.Watch(watchCtx, s.prefix, clientv3.WithPrefix())

		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-watcher:
				if !ok {
					// Watcher channel closed
					return
				}

				if resp.Err() != nil {
					ch <- WatchEvent{Err: resp.Err()}
					return
				}

				// For per-host mode, we signal a reload is needed.
				// The caller should call Load() to get the updated data.
				// We only send one event per watch response to avoid duplicate reloads.
				if len(resp.Events) > 0 {
					select {
					case ch <- WatchEvent{
						Data:    nil, // Signal to reload
						Version: resp.Header.Revision,
					}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}

// Close is a no-op for perHostStorage as the client is managed externally.
func (s *perHostStorage) Close() error {
	return nil
}
