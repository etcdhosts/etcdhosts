package etcdhosts

import (
	"crypto/tls"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdConfig struct {
	UserName  string
	Password  string
	Endpoints []string
	Timeout   time.Duration
	TLSConfig *tls.Config
	HostsKey  string
}

func (e *EtcdConfig) NewClient() (*clientv3.Client, error) {
	return clientv3.New(clientv3.Config{
		Username:    e.UserName,
		Password:    e.Password,
		Endpoints:   e.Endpoints,
		DialTimeout: 5 * time.Second,
		TLS:         e.TLSConfig,
	})
}
