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

const (
	GDNS_TYPE_A     = "TYPE_A"
	GDNS_TYPE_AAAA  = "TYPE_AAAA"
	GDNS_TYPE_TXT   = "TYPE_TXT"
	GDNS_TYPE_CNAME = "TYPE_CNAME"
	GDNS_TYPE_PTR   = "TYPE_PTR"
	GDNS_TYPE_NS    = "TYPE_NS"
)

var errKeyNotFound = errors.New("key not found")
var errTooManyKeyFound = errors.New("too many key found")
var errQueryNotSupport = errors.New("query type not support")

type EtcdDnsRecord struct {
	Type    uint16   `json:"type"`
	Records []string `json:"records"`
	TTL     uint32   `json:"ttl"`
}

func checkGDNSQueryType(qType uint16) bool {
	switch qType {
	case dns.TypeA:
		fallthrough
	case dns.TypeAAAA:
		fallthrough
	case dns.TypeTXT:
		fallthrough
	case dns.TypeCNAME:
		fallthrough
	case dns.TypePTR:
		fallthrough
	case dns.TypeNS:
		return true
	default:
		return false
	}
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

	if !checkGDNSQueryType(req.QType()) {
		return nil, errQueryNotSupport
	}

	domainKey := path.Join(gDns.PathPrefix, req.QName())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	etcdResp, err := gDns.Client.Get(ctx, domainKey)
	if err != nil {
		return records, err
	}

	if etcdResp.Count == 0 {
		return records, errKeyNotFound
	}

	if etcdResp.Count > 1 {
		return records, errTooManyKeyFound
	}

	kv := etcdResp.Kvs[0]

	for _, k := range etcdResp.Kvs {

		var etcdRecord EtcdDnsRecord
		if err := jsoniter.Unmarshal(k.Value, &etcdRecord); err != nil {
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

func reverse(ss []string) []string {
	for i := len(ss)/2 - 1; i >= 0; i-- {
		opp := len(ss) - 1 - i
		ss[i], ss[opp] = ss[opp], ss[i]
	}
	return ss
}
