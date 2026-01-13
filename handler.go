package etcdhosts

import (
	"context"
	"net"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"

	"github.com/etcdhosts/etcdhosts/v2/internal/healthcheck"
	"github.com/etcdhosts/etcdhosts/v2/internal/hosts"
	"github.com/etcdhosts/etcdhosts/v2/internal/loadbalance"
)

// EtcdHosts is the plugin handler for etcdhosts.
type EtcdHosts struct {
	Next     plugin.Handler
	Origins  []string
	Fall     fall.F
	TTL      uint32
	store    *hosts.Store
	checker  *healthcheck.Checker
	balancer *loadbalance.WeightedBalancer
}

// ServeDNS implements the plugin.Handler interface.
func (e *EtcdHosts) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()
	qtype := state.QType()

	// Track query duration
	start := time.Now()
	defer func() {
		queryDuration.WithLabelValues(qtypeString(qtype)).Observe(time.Since(start).Seconds())
	}()

	var answers []dns.RR

	zone := plugin.Zones(e.Origins).Matches(qname)
	if zone == "" {
		// PTR zones don't need to be specified in Origins.
		if qtype != dns.TypePTR {
			return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
		}
	}

	switch qtype {
	case dns.TypePTR:
		addr := dnsutil.ExtractAddressFromReverse(qname)
		names := e.store.LookupAddr(addr)
		if len(names) == 0 {
			queriesTotal.WithLabelValues("PTR", resultMiss).Inc()
			return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
		}
		answers = ptr(qname, e.TTL, names)
		queriesTotal.WithLabelValues("PTR", resultHit).Inc()

	case dns.TypeA:
		entries := e.store.LookupV4WithWildcard(qname)
		ips := e.filterAndBalance(qname, entries)
		if len(ips) > 0 {
			answers = a(qname, e.getTTL(entries), ips)
			queriesTotal.WithLabelValues("A", resultHit).Inc()
		} else {
			queriesTotal.WithLabelValues("A", resultMiss).Inc()
		}

	case dns.TypeAAAA:
		entries := e.store.LookupV6WithWildcard(qname)
		ips := e.filterAndBalance(qname, entries)
		if len(ips) > 0 {
			answers = aaaa(qname, e.getTTL(entries), ips)
			queriesTotal.WithLabelValues("AAAA", resultHit).Inc()
		} else {
			queriesTotal.WithLabelValues("AAAA", resultMiss).Inc()
		}
	}

	// Handle empty response
	if len(answers) == 0 && !e.otherRecordsExist(qname) {
		if e.Fall.Through(qname) {
			return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
		}
		// Return SERVFAIL since we don't have SOA records
		return dns.RcodeServerFailure, nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	_ = w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

// Name implements the plugin.Handler interface.
func (e *EtcdHosts) Name() string { return pluginName }

// filterAndBalance applies health check filtering and weighted load balancing.
func (e *EtcdHosts) filterAndBalance(hostname string, entries []hosts.Entry) []net.IP {
	if len(entries) == 0 {
		return nil
	}

	// Convert to loadbalance.Entry with health status
	lbEntries := make([]loadbalance.Entry, 0, len(entries))
	allUnhealthy := true

	for _, entry := range entries {
		healthy := true
		if e.checker != nil {
			healthy = e.checker.IsHealthy(hostname, entry.IP)
			// Update health check metric
			status := float64(0)
			if healthy {
				status = 1
			}
			healthcheckStatus.WithLabelValues(hostname, entry.IP.String()).Set(status)
		}

		if healthy {
			allUnhealthy = false
		}

		lbEntries = append(lbEntries, loadbalance.Entry{
			IP:      entry.IP,
			Weight:  entry.Weight,
			Healthy: healthy,
		})
	}

	// Apply unhealthy policy
	if allUnhealthy && e.checker != nil {
		switch e.checker.GetPolicy() {
		case healthcheck.PolicyReturnEmpty:
			return nil
		case healthcheck.PolicyFallthrough:
			return nil // Will trigger fallthrough in caller
		case healthcheck.PolicyReturnAll:
			// Mark all as healthy for this request
			for i := range lbEntries {
				lbEntries[i].Healthy = true
			}
		}
	}

	// Apply weighted load balancing
	return e.balancer.Select(lbEntries)
}

// getTTL returns the TTL to use for response records.
// Uses entry-specific TTL if set, otherwise falls back to default.
func (e *EtcdHosts) getTTL(entries []hosts.Entry) uint32 {
	if len(entries) > 0 && entries[0].TTL > 0 {
		return entries[0].TTL
	}
	return e.TTL
}

// otherRecordsExist checks if there are any records for the hostname.
func (e *EtcdHosts) otherRecordsExist(qname string) bool {
	if len(e.store.LookupV4WithWildcard(qname)) > 0 {
		return true
	}
	if len(e.store.LookupV6WithWildcard(qname)) > 0 {
		return true
	}
	return false
}

// qtypeString returns the string representation of a DNS query type.
func qtypeString(qtype uint16) string {
	switch qtype {
	case dns.TypeA:
		return "A"
	case dns.TypeAAAA:
		return "AAAA"
	case dns.TypePTR:
		return "PTR"
	default:
		return "OTHER"
	}
}

// a creates A records from IPs.
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

// aaaa creates AAAA records from IPs.
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

// ptr creates PTR records from hostnames.
func ptr(zone string, ttl uint32, names []string) []dns.RR {
	answers := make([]dns.RR, len(names))
	for i, n := range names {
		r := new(dns.PTR)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl}
		r.Ptr = dns.Fqdn(n)
		answers[i] = r
	}
	return answers
}
