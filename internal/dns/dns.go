// Package dns answers only configured local zones, without upstream forwarding.
package dns

import (
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/router"
	mdns "github.com/miekg/dns"
	"net"
	"sort"
)

type Handler struct {
	zones  []string
	routes *router.Router
	ip     net.IP
}

func New(c config.Config) *Handler {
	ip, _, _ := net.SplitHostPort(c.Proxy.Listen)
	zones := append([]string(nil), c.DNS.Zones...)
	sort.Slice(zones, func(i, j int) bool { return len(zones[i]) > len(zones[j]) })
	return &Handler{zones: zones, routes: router.New(c.Installations), ip: net.ParseIP(ip).To4()}
}
func (h *Handler) ServeDNS(w mdns.ResponseWriter, q *mdns.Msg) {
	m := new(mdns.Msg)
	m.SetReply(q)
	m.RecursionAvailable = false
	if q.Opcode != mdns.OpcodeQuery {
		m.Rcode = mdns.RcodeNotImplemented
		_ = w.WriteMsg(m)
		return
	}
	if len(q.Question) != 1 {
		m.Rcode = mdns.RcodeFormatError
		_ = w.WriteMsg(m)
		return
	}
	question := q.Question[0]
	name, e := config.Normalize(question.Name)
	zone := ""
	if e == nil {
		for _, z := range h.zones {
			if config.Within(name, z) {
				zone = z
				break
			}
		}
	}
	if zone == "" || question.Qclass != mdns.ClassINET {
		m.Rcode = mdns.RcodeRefused
		_ = w.WriteMsg(m)
		return
	}
	m.Authoritative = true
	_, matched := h.routes.Match(name)
	if !matched && name != router.DashboardHost {
		m.Rcode = mdns.RcodeNameError
	} else if question.Qtype == mdns.TypeA {
		m.Answer = []mdns.RR{&mdns.A{Hdr: mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 30}, A: h.ip}}
	}
	if len(m.Answer) == 0 {
		m.Ns = []mdns.RR{&mdns.SOA{Hdr: mdns.RR_Header{Name: mdns.Fqdn(zone), Rrtype: mdns.TypeSOA, Class: mdns.ClassINET, Ttl: 30}, Ns: "ns." + mdns.Fqdn(zone), Mbox: "hostmaster." + mdns.Fqdn(zone), Serial: 1, Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 30}}
	}
	_ = w.WriteMsg(m)
}
