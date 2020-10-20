package hosts

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.etcd.io/etcd/clientv3"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	mwtls "github.com/coredns/coredns/plugin/pkg/tls"

	"github.com/caddyserver/caddy"
)

var log = clog.NewWithPlugin("etcdhosts")

func init() { plugin.Register("etcdhosts", setup) }

func periodicHostsUpdate(h *Hosts) chan bool {
	parseChan := make(chan bool)

	go func() {
		watchCh := h.etcdClient.Watch(context.Background(), h.etcdHostsKey)
		for {
			select {
			case <-parseChan:
				return
			case <-watchCh:
				log.Info("etcdhosts reloading...")
				h.readHosts()
			}
		}
	}()
	return parseChan
}

func setup(c *caddy.Controller) error {
	h, err := hostsParse(c)
	if err != nil {
		return plugin.Error("etcdhosts", err)
	}

	parseChan := periodicHostsUpdate(&h)

	c.OnStartup(func() error {
		h.readHosts()
		return nil
	})

	c.OnShutdown(func() error {
		close(parseChan)
		_ = h.etcdClient.Close()
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		h.Next = next
		return h
	})

	return nil
}

func hostsParse(c *caddy.Controller) (Hosts, error) {
	h := Hosts{
		Hostsfile: &Hostsfile{
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
					return h, c.Errf("failed to load etcd tls config: %s", err.Error())
				}
				h.etcdTLSConfig = tlsConfig
			case "endpoint":
				remaining := c.RemainingArgs()
				if len(remaining) == 0 {
					return h, c.ArgErr()
				}
				h.etcdEndpoints = remaining
			case "timeout":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("etcd client timeout needs a duration")
				}
				timeout, err := time.ParseDuration(remaining[0])
				if err != nil {
					return h, c.Errf("invalid duration for etcd client timeout '%s'", remaining[0])
				}
				h.etcdTimeout = timeout
			case "key":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("etcd hosts key needs a string")
				}
				h.etcdHostsKey = remaining[0]
			case "credentials":
				remaining := c.RemainingArgs()
				if len(remaining) == 0 {
					return h, c.ArgErr()
				}
				if len(remaining) != 2 {
					return h, c.Errf("credentials requires 2 arguments, username and password")
				}
				h.etcdUserName, h.etcdPassword = remaining[0], remaining[1]
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
	if h.etcdHostsKey == "" {
		h.etcdHostsKey = "/etcdhosts"
	}

	// default etcd client timeout
	if h.etcdTimeout == 0 {
		h.etcdTimeout = 3 * time.Second
	}

	cli, err := clientv3.New(clientv3.Config{
		Username:    h.etcdUserName,
		Password:    h.etcdPassword,
		Endpoints:   h.etcdEndpoints,
		DialTimeout: 5 * time.Second,
		TLS:         h.etcdTLSConfig,
	})
	if err != nil {
		return h, c.Errf("failed to create etcd client: %s", err.Error())
	}
	h.etcdClient = cli

	h.initInline(inline)

	return h, nil
}
