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
	"runtime"
	"strings"
	"sync"
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
	c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test", "internal"}, Listen: "127.0.0.43:53"}, Proxy: config.Proxy{Listen: "127.0.0.42:80"}, Routes: []config.Route{{Domain: "install.web.test", Target: strings.TrimPrefix(b.URL, "http://")}}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	fixtureListeners(t, &c)
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
	req, _ := http.NewRequest("GET", "http://"+c.Proxy.Listen+"/admin/?x=1", nil)
	req.Host = "deep.site.install.web.test"
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 2 * time.Second}
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "deep.site.install.web.test /admin/?x=1" {
		t.Fatalf("%s", body)
	}
}

func TestConcurrentFiftyHosts(t *testing.T) {
	c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test", "internal"}, Listen: "127.0.0.43:53"}, Proxy: config.Proxy{Listen: "127.0.0.42:80"}}
	for i := 0; i < 10; i++ {
		index := i
		b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%d %s %s", index, r.Host, r.URL.RequestURI())
		}))
		defer b.Close()
		zone := "test"
		if i >= 5 {
			zone = "internal"
		}
		c.Routes = append(c.Routes, config.Route{Domain: fmt.Sprintf("install%d.web.%s", i, zone), Target: strings.TrimPrefix(b.URL, "http://")})
	}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	fixtureListeners(t, &c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, c) }()
	defer func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, e := net.DialTimeout("tcp4", c.Proxy.Listen, 100*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	transport := &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 10}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	start := time.Now()
	var wg sync.WaitGroup
	for i, r := range c.Routes {
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(i, j int, r config.Route) {
				defer wg.Done()
				host := fmt.Sprintf("deep.site%d.%s", j, r.Domain)
				q := new(mdns.Msg)
				q.SetQuestion(host+".", mdns.TypeA)
				a, _, e := (&mdns.Client{Timeout: time.Second}).Exchange(q, c.DNS.Listen)
				if e != nil || len(a.Answer) != 1 {
					t.Errorf("DNS %s: %v", host, e)
					return
				}
				req, _ := http.NewRequest("GET", "http://"+c.Proxy.Listen+"/admin/?original=1", nil)
				req.Host = host
				res, e := client.Do(req)
				if e != nil {
					t.Error(e)
					return
				}
				defer res.Body.Close()
				b, e := io.ReadAll(res.Body)
				want := fmt.Sprintf("%d %s /admin/?original=1", i, host)
				if e != nil || string(b) != want {
					t.Errorf("%s: got %q want %q (%v)", host, b, want, e)
				}
			}(i, j, r)
		}
	}
	wg.Wait()
	t.Logf("50 concurrent DNS+HTTP requests completed in %s (diagnostic only)", time.Since(start))
}

// Keep ordinary macOS tests unprivileged: validate production config first,
// then inject high-port socket addresses into the internal server fixture.
// Real dedicated-address alias behavior belongs to privileged OS smoke tests.
func fixtureListeners(t *testing.T, c *config.Config) {
	proxyIP, dnsIP := "127.0.0.42", "127.0.0.43"
	if runtime.GOOS == "darwin" {
		proxyIP, dnsIP = "127.0.0.1", "127.0.0.1"
	}
	c.Proxy.Listen = port(t, proxyIP)
	c.DNS.Listen = port(t, dnsIP)
}
