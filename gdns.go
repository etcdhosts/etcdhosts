package gdns

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/coredns/coredns/request"

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

func (gDns *GDns) getARecord(req request.Request) ([]dns.RR, error) {
	var records []dns.RR

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	etcdResp, err := gDns.Client.Get(ctx, path.Join(gDns.PathPrefix, req.Name(), "A"))
	if err != nil {
		return records, err
	}
	if etcdResp.Count == 0 {
		return records, errKeyNotFound
	}

	for _, k := range etcdResp.Kvs {
		log.Info(k)
	}

	records = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{
			Name:   req.QName(),
			Rrtype: req.QType(),
			Class:  req.QClass(),
			Ttl:    600,
		},
		A: []byte("1.1.1.1"),
	}}

	return records, errKeyNotFound
}

func (gDns *GDns) getAAAARecord() ([]dns.RR, error) {
	return nil, errKeyNotFound
}
