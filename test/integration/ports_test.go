//go:build port80 && linux

package integration

import (
	"context"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/diagnostics"
	"github.com/cbspire/raymond/internal/server"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDedicatedPort80AndWildcardConflict(t *testing.T) {
	localhost, e := net.Listen("tcp4", "127.0.0.1:80")
	if e != nil {
		t.Fatal(e)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "localhost unchanged") }), ReadHeaderTimeout: time.Second}
	go httpServer.Serve(localhost)
	defer httpServer.Close()
	c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test"}, Listen: port(t, "127.0.0.43")}, Installations: []config.Installation{{Domain: "install.test", Target: "127.0.0.1:80"}}}
	if e = c.Validate(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, c) }()
	defer cancel()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		req, _ := http.NewRequest("GET", "http://127.0.0.42/", nil)
		req.Host = "site.install.test"
		res, e := client.Do(req)
		if e == nil {
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if string(b) != "localhost unchanged" {
				t.Fatal(string(b))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	res, e := client.Get("http://127.0.0.1/")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != "localhost unchanged" {
		t.Fatal(string(b))
	}
	cancel()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	httpServer.Close()
	wildcard, e := net.Listen("tcp4", "0.0.0.0:80")
	if e != nil {
		t.Fatal(e)
	}
	defer wildcard.Close()
	if e = diagnostics.Check(c); e == nil {
		t.Fatal("doctor failed to detect wildcard conflict")
	}
}
