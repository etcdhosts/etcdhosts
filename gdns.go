package gdns

import (
	"context"
	"errors"
	"net"
	"path"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/plugin/pkg/upstream"
	etcdcv3 "go.etcd.io/etcd/clientv3"
)

var errKeyNotFound = errors.New("key not found")
var errQueryNotSupport = errors.New("query type not support")

type EtcdDnsRecord struct {
	Type    uint16   `json:"type"`
	Records []string `json:"records"`
	TTL     uint32   `json:"ttl"`
}

type GDns struct {
	Next       plugin.Handler
	Fall       fall.F
	Zones      []string
	PathPrefix string
	Upstream   *upstream.Upstream
	Client     *etcdcv3.Client

	endpoints []string // Stored here as well, to aid in testing.
}

func (gDns *GDns) getRecord(req request.Request) ([]dns.RR, error) {
	var records []dns.RR
	var domainKey string

	switch req.QType() {
	case dns.TypeA:
		domainKey = path.Join(gDns.PathPrefix, req.Name(), "A")
	case dns.TypeAAAA:
		domainKey = path.Join(gDns.PathPrefix, req.Name(), "AAAA")
	case dns.TypeTXT:
		domainKey = path.Join(gDns.PathPrefix, req.Name(), "TXT")
	case dns.TypeCNAME:
		domainKey = path.Join(gDns.PathPrefix, req.Name(), "CNAME")
	case dns.TypePTR:
		domainKey = path.Join(gDns.PathPrefix, req.Name(), "PTR")
	case dns.TypeNS:
		domainKey = path.Join(gDns.PathPrefix, req.Name(), "NS")
	case dns.TypeMX:
		fallthrough
	case dns.TypeSRV:
		fallthrough
	case dns.TypeSOA:
		fallthrough
	default:
		return nil, errQueryNotSupport
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	etcdResp, err := gDns.Client.Get(ctx, domainKey)
	if err != nil {
		return records, err
	}
	if etcdResp.Count == 0 {
		return records, errKeyNotFound
	}

	for _, k := range etcdResp.Kvs {

		var etcdRecord EtcdDnsRecord
		err := jsoniter.Unmarshal(k.Value, &etcdRecord)
		if err != nil {
			log.Warningf("failed to unmarshal record %v", k.Value)
			continue
		}

		if etcdRecord.Type != req.QType() {
			log.Warningf("record type error, find [%d] expect [%d]", etcdRecord.Type, req.QType())
			continue
		}

		for _, v := range etcdRecord.Records {
			hdr := dns.RR_Header{
				Name:   req.QName(),
				Rrtype: req.QType(),
				Class:  req.QClass(),
				Ttl:    etcdRecord.TTL,
			}

			switch req.QType() {
			case dns.TypeA:
				records = append(records, &dns.A{
					Hdr: hdr,
					A:   net.ParseIP(v),
				})
			case dns.TypeAAAA:
				records = append(records, &dns.AAAA{
					Hdr:  hdr,
					AAAA: net.ParseIP(v),
				})
			case dns.TypeTXT:
				records = append(records, &dns.TXT{
					Hdr: hdr,
					Txt: []string{v},
				})
			case dns.TypeCNAME:
				records = append(records, &dns.CNAME{
					Hdr:    hdr,
					Target: v,
				})
			case dns.TypePTR:
				records = append(records, &dns.PTR{
					Hdr: hdr,
					Ptr: v,
				})
			case dns.TypeNS:
				records = append(records, &dns.NS{
					Hdr: hdr,
					Ns:  v,
				})
			}

		}
	}

	return records, nil
}
