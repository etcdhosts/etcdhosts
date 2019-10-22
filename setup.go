package gdns

import (
	"crypto/tls"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	mwtls "github.com/coredns/coredns/plugin/pkg/tls"
	"github.com/coredns/coredns/plugin/pkg/upstream"

	"github.com/caddyserver/caddy"
	etcdcv3 "go.etcd.io/etcd/clientv3"
)

var log = clog.NewWithPlugin("gdns")

func init() { plugin.Register("gdns", setup) }

func setup(c *caddy.Controller) error {
	e, err := etcdParse(c)
	if err != nil {
		return plugin.Error("gdns", err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		e.Next = next
		return e
	})

	return nil
}

func etcdParse(c *caddy.Controller) (*GDNS, error) {
	gDns := GDNS{PathPrefix: "/gdns"}
	var (
		tlsConfig *tls.Config
		err       error
		endpoints = []string{defaultEndpoint}
		username  string
		password  string
	)

	gDns.Upstream = upstream.New()

	for c.Next() {
		gDns.Zones = c.RemainingArgs()
		if len(gDns.Zones) == 0 {
			gDns.Zones = make([]string, len(c.ServerBlockKeys))
			copy(gDns.Zones, c.ServerBlockKeys)
		}
		for i, str := range gDns.Zones {
			gDns.Zones[i] = plugin.Host(str).Normalize()
		}

		for c.NextBlock() {
			switch c.Val() {
			case "fallthrough":
				gDns.Fall.SetZonesFromArgs(c.RemainingArgs())
			case "debug":
				/* it is a noop now */
			case "path":
				if !c.NextArg() {
					return &GDNS{}, c.ArgErr()
				}
				gDns.PathPrefix = c.Val()
			case "endpoint":
				args := c.RemainingArgs()
				if len(args) == 0 {
					return &GDNS{}, c.ArgErr()
				}
				endpoints = args
			case "tls": // cert key cacertfile
				args := c.RemainingArgs()
				tlsConfig, err = mwtls.NewTLSConfigFromArgs(args...)
				if err != nil {
					return &GDNS{}, err
				}
			case "credentials":
				args := c.RemainingArgs()
				if len(args) == 0 {
					return &GDNS{}, c.ArgErr()
				}
				if len(args) != 2 {
					return &GDNS{}, c.Errf("credentials requires 2 arguments, username and password")
				}
				username, password = args[0], args[1]
			default:
				if c.Val() != "}" {
					return &GDNS{}, c.Errf("unknown property '%s'", c.Val())
				}
			}
		}
		client, err := newEtcdClient(endpoints, tlsConfig, username, password)
		if err != nil {
			return &GDNS{}, err
		}
		gDns.Client = client
		gDns.endpoints = endpoints

		return &gDns, nil
	}
	return &GDNS{}, nil
}

func newEtcdClient(endpoints []string, tlsConfig *tls.Config, username, password string) (*etcdcv3.Client, error) {
	etcdCfg := etcdcv3.Config{
		Endpoints: endpoints,
		TLS:       tlsConfig,
	}
	if username != "" && password != "" {
		etcdCfg.Username = username
		etcdCfg.Password = password
	}
	cli, err := etcdcv3.New(etcdCfg)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

const defaultEndpoint = "http://localhost:2379"
