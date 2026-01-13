package etcdhosts

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"github.com/etcdhosts/etcdhosts/v2/internal/etcd"
	"github.com/etcdhosts/etcdhosts/v2/internal/healthcheck"
	"github.com/etcdhosts/etcdhosts/v2/internal/hosts"
	"github.com/etcdhosts/etcdhosts/v2/internal/loadbalance"
)

var log = clog.NewWithPlugin(pluginName)

// Default configuration values
const (
	defaultTTL     = 3600
	defaultTimeout = 5 * time.Second
)

// etcdHostsConfig holds the parsed configuration.
type etcdHostsConfig struct {
	endpoints       []string
	username        string
	password        string
	tlsConfig       *tls.Config
	timeout         time.Duration
	storageMode     etcd.StorageMode
	key             string
	ttl             uint32
	healthcheckCfg  *healthcheck.Config
	enableHealthCfg bool
}

func setup(c *caddy.Controller) error {
	eh, cfg, err := parseConfig(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	// Context for managing lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	// Create etcd client
	etcdClient, err := etcd.NewClient(&etcd.Config{
		Endpoints:   cfg.endpoints,
		Username:    cfg.username,
		Password:    cfg.password,
		TLSConfig:   cfg.tlsConfig,
		DialTimeout: cfg.timeout,
		Key:         cfg.key,
		Mode:        cfg.storageMode,
	})
	if err != nil {
		cancel()
		return plugin.Error(pluginName, err)
	}

	storage := etcdClient.Storage()

	c.OnStartup(func() error {
		// Load initial data from etcd
		if err := loadFromEtcd(ctx, storage, eh.store); err != nil {
			log.Warningf("Failed to load initial data from etcd: %v", err)
		}

		// Start etcd watcher in background
		go watchEtcd(ctx, storage, eh.store)

		// Start health checker if configured
		if cfg.enableHealthCfg && eh.checker != nil {
			go eh.checker.Start(ctx)
		}

		return nil
	})

	c.OnShutdown(func() error {
		cancel()
		if eh.checker != nil {
			eh.checker.Stop()
		}
		_ = storage.Close()
		_ = etcdClient.Close()
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		eh.Next = next
		return eh
	})

	return nil
}

func parseConfig(c *caddy.Controller) (*EtcdHosts, *etcdHostsConfig, error) {
	cfg := &etcdHostsConfig{
		timeout:     defaultTimeout,
		key:         etcd.DefaultKey,
		ttl:         defaultTTL,
		storageMode: etcd.ModeSingle,
	}

	eh := &EtcdHosts{
		TTL:      defaultTTL,
		store:    hosts.NewStore(),
		balancer: loadbalance.NewWeightedBalancer(),
	}

	i := 0
	for c.Next() {
		if i > 0 {
			return nil, nil, plugin.ErrOnce
		}
		i++

		// Parse zone origins from args
		args := c.RemainingArgs()
		eh.Origins = plugin.OriginsFromArgsOrServerBlock(args, c.ServerBlockKeys)

		for c.NextBlock() {
			switch c.Val() {
			case "endpoint":
				args := c.RemainingArgs()
				if len(args) == 0 {
					return nil, nil, c.ArgErr()
				}
				cfg.endpoints = args

			case "credentials":
				args := c.RemainingArgs()
				if len(args) != 2 {
					return nil, nil, c.Errf("credentials requires username and password")
				}
				cfg.username = args[0]
				cfg.password = args[1]

			case "tls":
				args := c.RemainingArgs()
				if len(args) < 1 || len(args) > 3 {
					return nil, nil, c.Errf("tls requires 1-3 arguments: cert [key] [ca]")
				}
				tlsCfg, err := parseTLS(args)
				if err != nil {
					return nil, nil, c.Errf("tls config error: %v", err)
				}
				cfg.tlsConfig = tlsCfg

			case "timeout":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				timeout, err := time.ParseDuration(args[0])
				if err != nil {
					return nil, nil, c.Errf("invalid timeout: %v", err)
				}
				cfg.timeout = timeout

			case "storage":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				switch args[0] {
				case "single":
					cfg.storageMode = etcd.ModeSingle
				case "perhost":
					cfg.storageMode = etcd.ModePerHost
				default:
					return nil, nil, c.Errf("invalid storage mode: %s", args[0])
				}

			case "key":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				cfg.key = args[0]

			case "ttl":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				ttl, err := strconv.Atoi(args[0])
				if err != nil || ttl <= 0 || ttl > 65535 {
					return nil, nil, c.Errf("invalid ttl: %s", args[0])
				}
				cfg.ttl = uint32(ttl)
				eh.TTL = uint32(ttl)

			case "fallthrough":
				eh.Fall.SetZonesFromArgs(c.RemainingArgs())

			case "healthcheck":
				cfg.enableHealthCfg = true
				hcCfg, err := parseHealthcheck(c)
				if err != nil {
					return nil, nil, err
				}
				cfg.healthcheckCfg = hcCfg
				eh.checker = healthcheck.NewChecker(hcCfg)

			default:
				return nil, nil, c.Errf("unknown property: %s", c.Val())
			}
		}
	}

	// Validate required config
	if len(cfg.endpoints) == 0 {
		return nil, nil, c.Errf("endpoint is required")
	}

	return eh, cfg, nil
}

func parseHealthcheck(c *caddy.Controller) (*healthcheck.Config, error) {
	cfg := healthcheck.DefaultConfig()

	for c.NextBlock() {
		switch c.Val() {
		case "interval":
			args := c.RemainingArgs()
			if len(args) != 1 {
				return nil, c.ArgErr()
			}
			interval, err := time.ParseDuration(args[0])
			if err != nil {
				return nil, c.Errf("invalid interval: %v", err)
			}
			cfg.Interval = interval

		case "timeout":
			args := c.RemainingArgs()
			if len(args) != 1 {
				return nil, c.ArgErr()
			}
			timeout, err := time.ParseDuration(args[0])
			if err != nil {
				return nil, c.Errf("invalid timeout: %v", err)
			}
			cfg.Timeout = timeout

		case "max_concurrent":
			args := c.RemainingArgs()
			if len(args) != 1 {
				return nil, c.ArgErr()
			}
			maxConcurrent, err := strconv.Atoi(args[0])
			if err != nil || maxConcurrent <= 0 {
				return nil, c.Errf("invalid max_concurrent: %s", args[0])
			}
			cfg.MaxConcurrent = maxConcurrent

		case "unhealthy_policy":
			args := c.RemainingArgs()
			if len(args) != 1 {
				return nil, c.ArgErr()
			}
			switch args[0] {
			case "return_all":
				cfg.UnhealthyPolicy = healthcheck.PolicyReturnAll
			case "return_empty":
				cfg.UnhealthyPolicy = healthcheck.PolicyReturnEmpty
			case "fallthrough":
				cfg.UnhealthyPolicy = healthcheck.PolicyFallthrough
			default:
				return nil, c.Errf("invalid unhealthy_policy: %s", args[0])
			}

		default:
			return nil, c.Errf("unknown healthcheck property: %s", c.Val())
		}
	}

	return cfg, nil
}

func parseTLS(args []string) (*tls.Config, error) {
	cfg := &tls.Config{}

	// Load certificate and key
	cert := args[0]
	key := cert
	if len(args) >= 2 {
		key = args[1]
	}

	certificate, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}
	cfg.Certificates = []tls.Certificate{certificate}

	// Load CA certificate if provided
	if len(args) >= 3 {
		caCert, err := os.ReadFile(args[2])
		if err != nil {
			return nil, err
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, err
		}
		cfg.RootCAs = caCertPool
	}

	return cfg, nil
}

func loadFromEtcd(ctx context.Context, storage etcd.Storage, store *hosts.Store) error {
	data, _, err := storage.Load(ctx)
	if err != nil {
		etcdSyncTotal.WithLabelValues(statusError).Inc()
		return err
	}

	if data == nil {
		log.Info("No hosts data found in etcd")
		entriesTotal.Set(0)
		etcdSyncTotal.WithLabelValues(statusSuccess).Inc()
		etcdLastSync.SetToCurrentTime()
		return nil
	}

	records, err := hosts.ParseRecords(data)
	if err != nil {
		etcdSyncTotal.WithLabelValues(statusError).Inc()
		return err
	}

	store.Update(records)
	entriesTotal.Set(float64(store.Len()))
	etcdSyncTotal.WithLabelValues(statusSuccess).Inc()
	etcdLastSync.SetToCurrentTime()

	log.Infof("Loaded %d host entries from etcd", store.Len())
	return nil
}

func watchEtcd(ctx context.Context, storage etcd.Storage, store *hosts.Store) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		eventCh := storage.Watch(ctx)
		for event := range eventCh {
			if event.Err != nil {
				log.Errorf("etcd watch error: %v", event.Err)
				etcdSyncTotal.WithLabelValues(statusError).Inc()
				break // Reconnect
			}

			var data []byte
			if event.Data != nil {
				data = event.Data
			} else {
				// For per-host mode, we need to reload
				var err error
				data, _, err = storage.Load(ctx)
				if err != nil {
					log.Errorf("Failed to reload from etcd: %v", err)
					etcdSyncTotal.WithLabelValues(statusError).Inc()
					continue
				}
			}

			if data == nil {
				store.Update(nil)
				entriesTotal.Set(0)
				etcdSyncTotal.WithLabelValues(statusSuccess).Inc()
				etcdLastSync.SetToCurrentTime()
				log.Info("etcd data cleared")
				continue
			}

			records, err := hosts.ParseRecords(data)
			if err != nil {
				log.Errorf("Failed to parse hosts data: %v", err)
				etcdSyncTotal.WithLabelValues(statusError).Inc()
				continue
			}

			store.Update(records)
			entriesTotal.Set(float64(store.Len()))
			etcdSyncTotal.WithLabelValues(statusSuccess).Inc()
			etcdLastSync.SetToCurrentTime()
			log.Infof("Reloaded %d host entries from etcd", store.Len())
		}

		// Small delay before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
