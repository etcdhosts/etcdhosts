package gdns

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// ServeDNS implements the plugin.Handler interface.
func (gDNS *GDNS) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	zone := plugin.Zones(gDNS.Zones).Matches(state.Name())
	if zone == "" {
		return plugin.NextOrFailure(gDNS.Name(), gDNS.Next, ctx, w, r)
	}

	records, err := gDNS.getRecord(state)

	if err != nil {
		log.Warning(err)
		if err == errKeyNotFound && gDNS.Fall.Through(state.Name()) {
			return plugin.NextOrFailure(gDNS.Name(), gDNS.Next, ctx, w, r)
		}
	}

	resp := new(dns.Msg)
	resp.SetReply(r)
	resp.Authoritative = true
	resp.Answer = append(resp.Answer, records...)
	err = w.WriteMsg(resp)
	if err != nil {
		log.Error(err)
	}

	return dns.RcodeSuccess, nil
}

// Name implements the Handler interface.
func (gDNS *GDNS) Name() string { return "gdns" }
