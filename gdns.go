package gdns

import (
	"context"
	"errors"
	"net"
	"path"
	"strings"
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
var errRecordNotFound = errors.New("record not found")
var errTooManyKeyFound = errors.New("too many key found")
var errQueryNotSupport = errors.New("query type not support")

type EtcdDNSRecord struct {
	Domain    string `json:"domain"`
	SubDomain string `json:"sub_domain"`
	Type      uint16 `json:"type"`
	Record    string `json:"record"`
	TTL       uint32 `json:"ttl"`
}

type EtcdDNSRecords map[uint16][]EtcdDNSRecord

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
	case dns.TypeNS:
		return true
	default:
		return false
	}
}

type GDNS struct {
	Next       plugin.Handler
	Fall       fall.F
	Zones      []string
	PathPrefix string
	Upstream   *upstream.Upstream
	Client     *etcdcv3.Client

	endpoints []string // Stored here as well, to aid in testing.
}

func (gDNS *GDNS) getRecord(req request.Request) ([]dns.RR, error) {

	var records []dns.RR

	if !checkGDNSQueryType(req.QType()) {
		return nil, errQueryNotSupport
	}

	ss := strings.FieldsFunc(req.QName(), func(r rune) bool { return r == '.' })
	if len(ss) < 2 {
		return records, nil
	}

	domain := ss[len(ss)-2] + "." + ss[len(ss)-1]
	subDomain := strings.Join(ss[:len(ss)-2], ".")
	if len(ss) == 2 {
		subDomain = "@"
	}

	domainKey := path.Join(gDNS.PathPrefix, domain)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	etcdResp, err := gDNS.Client.Get(ctx, domainKey)
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
	var etcdRecords EtcdDNSRecords
	if err := jsoniter.Unmarshal(kv.Value, &etcdRecords); err != nil {
		return records, err
	}

	rs := etcdRecords[req.QType()]
	if rs == nil {
		return records, errRecordNotFound
	}

	for _, r := range rs {
		if r.Domain == domain && r.SubDomain == subDomain {
			hdr := dns.RR_Header{
				Name:   req.QName(),
				Rrtype: req.QType(),
				Class:  req.QClass(),
				Ttl:    r.TTL,
			}

			switch req.QType() {
			case dns.TypeA:
				records = append(records, &dns.A{
					Hdr: hdr,
					A:   net.ParseIP(r.Record),
				})
			case dns.TypeAAAA:
				records = append(records, &dns.AAAA{
					Hdr:  hdr,
					AAAA: net.ParseIP(r.Record),
				})
			case dns.TypeTXT:
				records = append(records, &dns.TXT{
					Hdr: hdr,
					Txt: []string{r.Record},
				})
			case dns.TypeCNAME:
				records = append(records, &dns.CNAME{
					Hdr:    hdr,
					Target: r.Record,
				})
			case dns.TypeNS:
				records = append(records, &dns.NS{
					Hdr: hdr,
					Ns:  r.Record,
				})
			}
		}

	}

	if len(records) == 0 {
		return records, errRecordNotFound
	} else {
		return records, nil
	}

}
