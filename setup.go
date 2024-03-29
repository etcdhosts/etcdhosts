package etcdhosts

import (
	"context"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	mwtls "github.com/coredns/coredns/plugin/pkg/tls"

	"github.com/coredns/caddy"
)

var log = clog.NewWithPlugin("etcdhosts")

func init() { plugin.Register("etcdhosts", setup) }

func setup(c *caddy.Controller) error {
	h, err := hostsParse(c)
	if err != nil {
		return plugin.Error("etcdhosts", err)
	}

	updateCancel := h.periodicHostsUpdate()

	c.OnStartup(func() error {
		h.readEtcdHosts()
		return nil
	})

	c.OnShutdown(func() error {
		updateCancel()
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		h.Next = next
		return h
	})

	return nil
}

func hostsParse(c *caddy.Controller) (*EtcdHosts, error) {
	h := &EtcdHosts{
		HostsFile: &HostsFile{
			hmap:    newMap(),
			inline:  newMap(),
			options: newOptions(),
		},
		etcdConfig: &EtcdConfig{},
	}

	var inline []string
	i := 0
	for c.Next() {
		if i > 0 {
			return h, plugin.ErrOnce
		}
		i++

		h.Origins = plugin.OriginsFromArgsOrServerBlock(c.RemainingArgs(), c.ServerBlockKeys)

		for c.NextBlock() {
			switch c.Val() {
			case "fallthrough":
				h.Fall.SetZonesFromArgs(c.RemainingArgs())
			case "no_reverse":
				h.options.autoReverse = false
			case "ttl":
				remaining := c.RemainingArgs()
				if len(remaining) < 1 {
					return h, c.Errf("ttl needs a time in second")
				}
				ttl, err := strconv.Atoi(remaining[0])
				if err != nil {
					return h, c.Errf("ttl needs a number of second")
				}
				if ttl <= 0 || ttl > 65535 {
					return h, c.Errf("ttl provided is invalid")
				}
				h.options.ttl = uint32(ttl)
			case "tls":
				remaining := c.RemainingArgs()
				tlsConfig, err := mwtls.NewTLSConfigFromArgs(remaining...)
				if err != nil {
					return h, c.Errf("failed to load etcd tls config: %s", err.Error())
				}
				h.etcdConfig.TLSConfig = tlsConfig
			case "endpoint":
				remaining := c.RemainingArgs()
				if len(remaining) == 0 {
					return h, c.ArgErr()
				}
				h.etcdConfig.Endpoints = remaining
			case "timeout":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("timeout needs a duration")
				}
				timeout, err := time.ParseDuration(remaining[0])
				if err != nil {
					return h, c.Errf("invalid duration for timeout '%s'", remaining[0])
				}
				h.etcdConfig.Timeout = timeout
			case "key":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("etcd hosts key needs a string")
				}
				h.etcdConfig.HostsKey = remaining[0]
			case "credentials":
				remaining := c.RemainingArgs()
				if len(remaining) == 0 {
					return h, c.ArgErr()
				}
				if len(remaining) != 2 {
					return h, c.Errf("credentials requires 2 arguments, username and password")
				}
				h.etcdConfig.UserName, h.etcdConfig.Password = remaining[0], remaining[1]
			case "force_reload":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("force_reload needs a duration")
				}
				forceReload, err := time.ParseDuration(remaining[0])
				if err != nil {
					return h, c.Errf("invalid duration for force_reload '%s'", remaining[0])
				}
				h.etcdConfig.ForceReload = forceReload
			default:
				if len(h.Fall.Zones) == 0 {
					line := strings.Join(append([]string{c.Val()}, c.RemainingArgs()...), " ")
					inline = append(inline, line)
					continue
				}
				return h, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	// default etcd key
	if h.etcdConfig.HostsKey == "" {
		h.etcdConfig.HostsKey = "/etcdhosts"
	}

	// default etcd client timeout
	if h.etcdConfig.Timeout == 0 {
		h.etcdConfig.Timeout = 3 * time.Second
	}

	// create etcd client
	if err := h.initEtcdClient(); err != nil {
		return nil, c.Errf("failed to create etcd client: %s", err)
	}

	h.initInline(inline)
	return h, nil
}

func (h *EtcdHosts) periodicHostsUpdate() context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		reloadTick := make(<-chan time.Time)
		if h.etcdConfig.ForceReload > 0 {
			reloadTick = time.Tick(h.etcdConfig.ForceReload)
		}
		watchCh := h.etcdClient.Watch(clientv3.WithRequireLeader(context.Background()), h.etcdConfig.HostsKey)
		for {
			select {
			case <-ctx.Done():
				if err := h.closeClient(); err != nil {
					log.Errorf("etcdhosts client close failed: %s", err.Error())
				}
				return
			case <-time.Tick(1 * time.Minute):
				if err := h.syncEndpoints(); err != nil {
					log.Errorf("etcdhosts client sync error: %s", err.Error())
					continue
				}
				log.Infof("etcdhosts client endpoints sync success: %v", h.etcdClient.Endpoints())
			case <-reloadTick:
				log.Info("etcdhosts force reloading...")
				h.readEtcdHosts()
			case _, ok := <-watchCh:
				if !ok {
					log.Error("failed to watch etcd events: channel read failed")
					continue
				}
				log.Info("etcdhosts reloading...")
				h.readEtcdHosts()
			}
		}
	}()
	return cancel
}
