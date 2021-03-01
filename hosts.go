package etcdhosts

import (
	"context"
	"net"

	"go.etcd.io/etcd/clientv3"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// EtcdHosts is the plugin handler
type EtcdHosts struct {
	Next plugin.Handler
	*HostsFile
	etcdConfig *EtcdConfig
	etcdClient *clientv3.Client
	Fall       fall.F
}

// ServeDNS implements the plugin.Handle interface.
func (h EtcdHosts) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()

	var answers []dns.RR

	zone := plugin.Zones(h.Origins).Matches(qname)
	if zone == "" {
		// PTR zones don't need to be specified in Origins.
		if state.QType() != dns.TypePTR {
			// if this doesn't match we need to fall through regardless of h.Fallthrough
			return plugin.NextOrFailure(h.Name(), h.Next, ctx, w, r)
		}
	}

	switch state.QType() {
	case dns.TypePTR:
		names := h.LookupStaticAddr(dnsutil.ExtractAddressFromReverse(qname))
		if len(names) == 0 {
			// If this doesn't match we need to fall through regardless of h.Fallthrough
			return plugin.NextOrFailure(h.Name(), h.Next, ctx, w, r)
		}
		answers = h.ptr(qname, h.options.ttl, names)
	case dns.TypeA:
		ips := h.LookupStaticHostV4(qname)
		answers = a(qname, h.options.ttl, ips)
	case dns.TypeAAAA:
		ips := h.LookupStaticHostV6(qname)
		answers = aaaa(qname, h.options.ttl, ips)
	}

	if len(answers) == 0 {
		if h.Fall.Through(qname) {
			return plugin.NextOrFailure(h.Name(), h.Next, ctx, w, r)
		}
		// We want to send an NXDOMAIN, but because of /etc/hosts' setup we don't have a SOA, so we make it REFUSED
		// to at least give an answer back to signals we're having problems resolving this.
		if !h.otherRecordsExist(qname) {
			return dns.RcodeServerFailure, nil
		}
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	_ = w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

func (h EtcdHosts) otherRecordsExist(qname string) bool {
	if len(h.LookupStaticHostV4(qname)) > 0 {
		return true
	}
	if len(h.LookupStaticHostV6(qname)) > 0 {
		return true
	}
	return false
}

// Name implements the plugin.Handle interface.
func (h EtcdHosts) Name() string { return "etcdhosts" }

// a takes a slice of net.IPs and returns a slice of A RRs.
func a(zone string, ttl uint32, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}
		r.A = ip
		answers[i] = r
	}
	return answers
}

// aaaa takes a slice of net.IPs and returns a slice of AAAA RRs.
func aaaa(zone string, ttl uint32, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.AAAA)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl}
		r.AAAA = ip
		answers[i] = r
	}
	return answers
}

// ptr takes a slice of host names and filters out the ones that aren't in Origins, if specified, and returns a slice of PTR RRs.
func (h *EtcdHosts) ptr(zone string, ttl uint32, names []string) []dns.RR {
	answers := make([]dns.RR, len(names))
	for i, n := range names {
		r := new(dns.PTR)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl}
		r.Ptr = dns.Fqdn(n)
		answers[i] = r
	}
	return answers
}

func (h *EtcdHosts) readEtcdHosts() {
	ctx, cancel := context.WithTimeout(context.Background(), h.etcdConfig.Timeout)
	defer cancel()

	getResp, err := h.etcdClient.Get(ctx, h.etcdConfig.HostsKey)
	if err != nil {
		log.Errorf("failed to get etcdConfig key [%s]: %s", h.etcdConfig.HostsKey, err.Error())
		return
	}

	if len(getResp.Kvs) != 1 {
		log.Errorf("invalid etcdConfig response: %d", len(getResp.Kvs))
		return
	}

	h.readHosts(getResp.Kvs[0].Value, getResp.Kvs[0].Version)
}
