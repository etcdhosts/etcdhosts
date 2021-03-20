package etcdhosts

import (
	"context"
	"strconv"
	"strings"
	"time"

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

	parseChan := h.periodicHostsUpdate()

	c.OnStartup(func() error {
		h.readEtcdHosts()
		return nil
	})

	c.OnShutdown(func() error {
		close(parseChan)
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		h.Next = next
		return h
	})

	return nil
}

func hostsParse(c *caddy.Controller) (EtcdHosts, error) {
	h := EtcdHosts{
		HostsFile: &HostsFile{
			hmap:    newMap(),
			inline:  newMap(),
			options: newOptions(),
		},
	}

	var inline []string
	i := 0
	for c.Next() {
		if i > 0 {
			return h, plugin.ErrOnce
		}
		i++

		origins := make([]string, len(c.ServerBlockKeys))
		copy(origins, c.ServerBlockKeys)
		args := c.RemainingArgs()
		if len(args) > 0 {
			origins = args
		}

		for i := range origins {
			origins[i] = plugin.Host(origins[i]).Normalize()
		}
		h.Origins = origins

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
					return h, c.Errf("failed to load etcdConfig tls config: %s", err.Error())
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
					return h, c.Errf("etcdConfig client timeout needs a duration")
				}
				timeout, err := time.ParseDuration(remaining[0])
				if err != nil {
					return h, c.Errf("invalid duration for etcdConfig client timeout '%s'", remaining[0])
				}
				h.etcdConfig.Timeout = timeout
			case "key":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("etcdConfig hosts key needs a string")
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

	// default etcdConfig key
	if h.etcdConfig.HostsKey == "" {
		h.etcdConfig.HostsKey = "/etcdhosts"
	}

	// default etcdConfig client timeout
	if h.etcdConfig.Timeout == 0 {
		h.etcdConfig.Timeout = 3 * time.Second
	}

	cli, err := h.etcdConfig.NewClient()
	if err != nil {
		log.Fatalf("failed to create etcdConfig client: %w", err)
	}
	h.etcdClient = cli

	h.initInline(inline)
	return h, nil
}

func (h *EtcdHosts) periodicHostsUpdate() chan bool {
	parseChan := make(chan bool)

	go func() {
	StartWatch:
		tick := time.Tick(30 * time.Second)
		watchCh := h.etcdClient.Watch(context.Background(), h.etcdConfig.HostsKey)
		for {
			select {
			case <-parseChan:
				return
			case <-tick:
				ctx, syncCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer syncCancel()

				err := h.etcdClient.Sync(ctx)
				if err != nil {
					log.Warningf("etcd client sync error(%s), try to reconnect...", err.Error())
					cli, err := h.etcdConfig.NewClient()
					if err != nil {
						log.Errorf("etcd client is closed, reconnect failed: %w", err)
						continue
					}
					h.Lock()
					h.etcdClient = cli
					h.Unlock()
					log.Warning("etcd client is closed, reconnect success...")
					goto StartWatch
				}
				log.Info("etcd client endpoints sync success")
			case _, ok := <-watchCh:
				if ok {
					log.Info("etcdhosts reloading...")
					h.readEtcdHosts()
				}
			}
		}
	}()
	return parseChan
}
