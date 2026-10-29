package integration

import (
	"context"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/server"
	mdns "github.com/miekg/dns"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func port(t *testing.T, ip string) string {
	t.Helper()
	l, e := net.Listen("tcp4", ip+":0")
	if e != nil {
		t.Fatal(e)
	}
	a := l.Addr().String()
	l.Close()
	return a
}
func TestForegroundDNSAndHTTP(t *testing.T) {
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Host+" "+r.URL.RequestURI()) }))
	defer b.Close()
	c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test", "internal"}, Listen: port(t, "127.0.0.43")}, Proxy: config.Proxy{Listen: port(t, "127.0.0.42")}, Installations: []config.Installation{{Domain: "install.web.test", Target: strings.TrimPrefix(b.URL, "http://")}}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, c) }()
	defer func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(12 * time.Second):
			t.Error("shutdown timed out")
		}
	}()
	q := new(mdns.Msg)
	q.SetQuestion("deep.site.install.web.test.", mdns.TypeA)
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, _, e := (&mdns.Client{Timeout: 100 * time.Millisecond}).Exchange(q, c.DNS.Listen)
		if e == nil && len(res.Answer) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("DNS not ready: %v", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, network := range []string{"udp", "tcp"} {
		res, _, e := (&mdns.Client{Net: network, Timeout: time.Second}).Exchange(q, c.DNS.Listen)
		if e != nil || len(res.Answer) != 1 {
			t.Fatalf("%s: %v %v", network, res, e)
		}
	}
	req, _ := http.NewRequest("GET", "http://"+c.Proxy.Listen+"/wp-admin/?x=1", nil)
	req.Host = "deep.site.install.web.test"
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 2 * time.Second}
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "deep.site.install.web.test /wp-admin/?x=1" {
		t.Fatalf("%s", body)
	}
}
