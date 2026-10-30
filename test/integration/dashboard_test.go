package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/server"
	mdns "github.com/miekg/dns"
)

func TestGeneratedConfigServesDashboard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_UID", "")
	p, _, e := config.InitUser()
	if e != nil {
		t.Fatal(e)
	}
	c, e := config.Load(p)
	if e != nil {
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
	q.SetQuestion("raymond.test.", mdns.TypeA)
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, _, e := (&mdns.Client{Timeout: 100 * time.Millisecond}).Exchange(q, c.DNS.Listen)
		if e == nil && len(r.Answer) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dashboard DNS did not become ready: %v", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	r, e := http.NewRequest("GET", "http://"+c.Proxy.Listen+"/", nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Host = "raymond.test"
	response, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, e := io.ReadAll(response.Body)
	if e != nil || response.StatusCode != 200 || !strings.Contains(string(body), "At your service.") {
		t.Fatalf("dashboard response: HTTP %d, %v, %s", response.StatusCode, e, body)
	}
}
