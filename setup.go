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
			case "force_start":
				remaining := c.RemainingArgs()
				if len(remaining) != 1 {
					return h, c.Errf("etcdConfig client force_start needs a boolean")
				}
				forceStart, err := strconv.ParseBool(remaining[0])
				if err != nil {
					return h, c.Errf("invalid boolean for etcdConfig client force_start '%s'", remaining[0])
				}
				h.etcdConfig.ForceStart = forceStart
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

	var err error

	// create etcd client
	if err = h.newClient(); err != nil {
		if h.etcdConfig.ForceStart {
			log.Errorf("failed to create etcdConfig client: %s", err)
		} else {
			log.Fatalf("failed to create etcdConfig client: %s", err)
		}
	}

	// sync etcd client endpoints
	if err = h.syncEndpoints(); err != nil {
		if h.etcdConfig.ForceStart {
			log.Errorf("failed to connect etcd server(sync error): %v", err)
		} else {
			log.Fatalf("failed to connect etcd server(sync error): %v", err)
		}
	}

	h.initInline(inline)
	return h, nil
}

func (h *EtcdHosts) periodicHostsUpdate() chan bool {
	parseChan := make(chan bool)

	go func() {
	CONNECT:
		var err error
		tick := time.Tick(30 * time.Second)
		if h.etcdClient == nil {
			for range tick {
				if err = h.reconnect(); err != nil {
					log.Errorf("etcdhosts client reconnect failed: %s", err)
				}
				break
			}
		}
		watchCh := h.etcdClient.Watch(context.Background(), h.etcdConfig.HostsKey)
		for {
			select {
			case <-parseChan:
				if err = h.closeClient(); err != nil {
					log.Errorf("etcdhosts client close failed: %s", err.Error())
				}
				return
			case <-tick:
				if err = h.syncEndpoints(); err != nil {
					log.Errorf("etcdhosts client sync error(%s), try to reconnect...", err.Error())
					if err = h.reconnect(); err != nil {
						log.Errorf("etcdhosts client reconnect failed: %s", err)
						continue
					}
					goto CONNECT
				}
				log.Infof("etcdhosts client endpoints sync success: %v", h.etcdClient.Endpoints())
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
