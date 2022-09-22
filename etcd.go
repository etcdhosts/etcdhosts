package etcdhosts

import (
	"crypto/tls"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdConfig struct {
	UserName   string
	Password   string
	Endpoints  []string
	Timeout    time.Duration
	TLSConfig  *tls.Config
	HostsKey   string
	ForceStart bool
}

func (c *EtcdConfig) NewClient() (*clientv3.Client, error) {
	return clientv3.New(clientv3.Config{
		Username:    c.UserName,
		Password:    c.Password,
		Endpoints:   c.Endpoints,
		DialTimeout: 3 * time.Second,
		TLS:         c.TLSConfig,
	})
}
