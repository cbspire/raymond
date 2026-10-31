package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/server"
)

func TestShutdownCancelsActiveBackendRequests(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(canceled) }))
	defer backend.Close()
	cfg := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test"}}, Routes: []config.Route{{Domain: "app.test", Target: backend.Listener.Addr().String()}}}
	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}
	fixtureListeners(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, cfg) }()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		deadline := time.Now().Add(2 * time.Second)
		for {
			request, _ := http.NewRequest("GET", "http://"+cfg.Proxy.Listen+"/", nil)
			request.Host = "app.test"
			response, e := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if e == nil || time.Now().After(deadline) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("backend was not canceled by shutdown")
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server shutdown blocked on active request")
	}
	<-requestDone
}
