package dns

import (
	"github.com/cbspire/raymond/internal/config"
	mdns "github.com/miekg/dns"
	"net"
	"testing"
)

func TestAnswers(t *testing.T) {
	c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test", "internal"}}, Installations: []config.Installation{{Domain: "install.web.test", Target: "127.0.0.1:9001"}, {Domain: "demo.internal", Target: "127.0.0.1:9002"}}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	h := New(c)
	for _, tc := range []struct {
		name         string
		typ          uint16
		rcode, count int
	}{{"site.install.web.test.", mdns.TypeA, 0, 1}, {"deep.eu.site.install.web.test.", mdns.TypeA, 0, 1}, {"install.web.test.", mdns.TypeA, 0, 1}, {"DEMO.INTERNAL.", mdns.TypeA, 0, 1}, {"evilinstall.web.test.", mdns.TypeA, mdns.RcodeNameError, 0}, {"unknown.test.", mdns.TypeA, mdns.RcodeNameError, 0}, {"site.install.web.test.evil.", mdns.TypeA, mdns.RcodeRefused, 0}, {"site.install.web.test.", mdns.TypeAAAA, 0, 0}, {"site.install.web.test.", mdns.TypeTXT, 0, 0}, {"site.install.web.test.", mdns.TypeANY, 0, 0}} {
		t.Run(tc.name+mdns.TypeToString[tc.typ], func(t *testing.T) {
			m := new(mdns.Msg)
			m.SetQuestion(tc.name, tc.typ)
			w := &writer{}
			h.ServeDNS(w, m)
			if w.msg.Rcode != tc.rcode || len(w.msg.Answer) != tc.count || w.msg.RecursionAvailable {
				t.Fatal(w.msg)
			}
			if tc.count == 1 && w.msg.Answer[0].(*mdns.A).A.String() != "127.0.0.42" {
				t.Fatal(w.msg)
			}
			if tc.rcode != mdns.RcodeRefused && !w.msg.Authoritative {
				t.Fatal("missing AA")
			}
		})
	}
	m := new(mdns.Msg)
	m.SetQuestion("install.web.test.", mdns.TypeA)
	m.Question = append(m.Question, m.Question[0])
	w := &writer{}
	h.ServeDNS(w, m)
	if w.msg.Rcode != mdns.RcodeFormatError {
		t.Fatal(w.msg)
	}
}

type writer struct{ msg *mdns.Msg }

func TestDashboardDNS(t *testing.T) {
	for _, tc := range []struct {
		zone, host   string
		typ          uint16
		rcode, count int
	}{
		{"raymond.test", "raymond.test.", mdns.TypeA, 0, 1},
		{"test", "RAYMOND.TEST.", mdns.TypeA, 0, 1},
		{"raymond.test", "raymond.test.", mdns.TypeAAAA, 0, 0},
		{"raymond.test", "unknown.raymond.test.", mdns.TypeA, mdns.RcodeNameError, 0},
		{"internal", "raymond.test.", mdns.TypeA, mdns.RcodeRefused, 0},
	} {
		t.Run(tc.zone+tc.host+mdns.TypeToString[tc.typ], func(t *testing.T) {
			c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{tc.zone}}}
			if e := c.Validate(); e != nil {
				t.Fatal(e)
			}
			q := new(mdns.Msg)
			q.SetQuestion(tc.host, tc.typ)
			w := &writer{}
			New(c).ServeDNS(w, q)
			if w.msg.Rcode != tc.rcode || len(w.msg.Answer) != tc.count {
				t.Fatal(w.msg)
			}
			if tc.count > 0 && w.msg.Answer[0].(*mdns.A).A.String() != "127.0.0.42" {
				t.Fatal(w.msg)
			}
		})
	}
}

func (w *writer) LocalAddr() net.Addr         { return &net.UDPAddr{} }
func (w *writer) RemoteAddr() net.Addr        { return &net.UDPAddr{} }
func (w *writer) WriteMsg(m *mdns.Msg) error  { w.msg = m; return nil }
func (w *writer) Write(b []byte) (int, error) { return len(b), nil }
func (w *writer) Close() error                { return nil }
func (w *writer) TsigStatus() error           { return nil }
func (w *writer) TsigTimersOnly(bool)         {}
func (w *writer) Hijack()                     {}
