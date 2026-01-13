package etcd

import (
	"context"
	"crypto/tls"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// DefaultDialTimeout is the default timeout for connecting to etcd.
	DefaultDialTimeout = 5 * time.Second
	// DefaultKey is the default etcd key for hosts data.
	DefaultKey = "/etcdhosts"
)

// Config holds the configuration for connecting to etcd.
type Config struct {
	Endpoints   []string      // etcd endpoints (required)
	Username    string        // etcd username for authentication
	Password    string        // etcd password for authentication
	TLSConfig   *tls.Config   // TLS configuration for secure connections
	DialTimeout time.Duration // timeout for initial connection (default 5s)
	Key         string        // etcd key or prefix (default /etcdhosts)
	Mode        StorageMode   // storage mode (default single)
}

// Client wraps the etcd client and provides storage access.
type Client struct {
	client *clientv3.Client
	key    string
	mode   StorageMode
}

// NewClient creates a new etcd client with the given configuration.
func NewClient(cfg *Config) (*Client, error) {
	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = DefaultDialTimeout
	}

	key := cfg.Key
	if key == "" {
		key = DefaultKey
	}

	// Ensure key starts with /
	if !strings.HasPrefix(key, "/") {
		key = "/" + key
	}

	etcdCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: dialTimeout,
		Username:    cfg.Username,
		Password:    cfg.Password,
		TLS:         cfg.TLSConfig,
	}

	client, err := clientv3.New(etcdCfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: client,
		key:    key,
		mode:   cfg.Mode,
	}, nil
}

// Storage returns a Storage implementation based on the configured mode.
func (c *Client) Storage() Storage {
	switch c.mode {
	case ModePerHost:
		// Ensure prefix ends with / for per-host mode
		prefix := c.key
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		return newPerHostStorage(c.client, prefix)
	default:
		return newSingleKeyStorage(c.client, c.key)
	}
}

// Sync synchronizes the client's endpoints with the cluster's current membership.
// This should be called periodically to keep the client up-to-date with cluster changes.
func (c *Client) Sync(ctx context.Context) error {
	return c.client.Sync(ctx)
}

// Close releases all resources held by the client.
func (c *Client) Close() error {
	return c.client.Close()
}

// Endpoints returns the current list of endpoints.
func (c *Client) Endpoints() []string {
	return c.client.Endpoints()
}

// Key returns the configured etcd key.
func (c *Client) Key() string {
	return c.key
}

// Mode returns the configured storage mode.
func (c *Client) Mode() StorageMode {
	return c.mode
}
