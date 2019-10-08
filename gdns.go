package gdns

import (
	"errors"

	"github.com/miekg/dns"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/plugin/pkg/upstream"
	etcdcv3 "go.etcd.io/etcd/clientv3"
)

var errKeyNotFound = errors.New("key not found")

type GDns struct {
	Next       plugin.Handler
	Fall       fall.F
	Zones      []string
	PathPrefix string
	Upstream   *upstream.Upstream
	Client     *etcdcv3.Client

	endpoints []string // Stored here as well, to aid in testing.
}

func (gDns *GDns) getARecord(resp dns.Msg) ([]dns.RR, error) {
	return nil, errKeyNotFound
}

func (gDns *GDns) getAAAARecord() ([]dns.RR, error) {
	return nil, errKeyNotFound
}
