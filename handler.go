package gdns

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// ServeDNS implements the plugin.Handler interface.
func (gDns *GDns) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	log.Info(state)
	zone := plugin.Zones(gDns.Zones).Matches(state.Name())
	if zone == "" {
		return plugin.NextOrFailure(gDns.Name(), gDns.Next, ctx, w, r)
	}

	var (
		records, extra []dns.RR
		err            error
	)

	switch state.QType() {
	case dns.TypeA:
		records, err = gDns.getARecord(state)
	case dns.TypeAAAA:
		records, err = gDns.getAAAARecord()
	case dns.TypeTXT:
	case dns.TypeCNAME:
	case dns.TypePTR:
	case dns.TypeMX:
	case dns.TypeSRV:
	case dns.TypeSOA:
	case dns.TypeNS:
	default:

	}

	if err != nil {
		if err == errKeyNotFound {
			return plugin.NextOrFailure(gDns.Name(), gDns.Next, ctx, w, r)
		} else {
			return dns.RcodeBadName, err
		}
	}

	resp := new(dns.Msg)
	resp.SetReply(r)
	resp.Authoritative = true
	resp.Answer = append(resp.Answer, records...)
	resp.Extra = append(resp.Extra, extra...)

	w.WriteMsg(resp)
	return dns.RcodeSuccess, nil
}

// Name implements the Handler interface.
func (gDns *GDns) Name() string { return "gdns" }
