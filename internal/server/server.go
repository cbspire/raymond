// Package server coordinates foreground serving and graceful termination.
package server

import (
	"context"
	"errors"
	"github.com/cbspire/raymond/internal/config"
	rdns "github.com/cbspire/raymond/internal/dns"
	"github.com/cbspire/raymond/internal/proxy"
	"github.com/cbspire/raymond/internal/router"
	mdns "github.com/miekg/dns"
	"net"
	"net/http"
	"time"
)

func Run(ctx context.Context, c config.Config) error {
	// Acquire all sockets before starting any service; a partial startup serves nothing.
	l, e := net.Listen("tcp4", c.Proxy.Listen)
	if e != nil {
		return e
	}
	defer l.Close()
	tcp, e := net.Listen("tcp4", c.DNS.Listen)
	if e != nil {
		return e
	}
	defer tcp.Close()
	udp, e := net.ListenPacket("udp4", c.DNS.Listen)
	if e != nil {
		return e
	}
	defer udp.Close()
	h := rdns.New(c)
	ds := []*mdns.Server{{Listener: tcp, Handler: h, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}, {PacketConn: udp, Handler: h, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, UDPSize: 1232}}
	s := &http.Server{Handler: proxy.New(router.New(c.Routes)), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 3)
	go func() { done <- s.Serve(l) }()
	for _, d := range ds {
		go func(d *mdns.Server) { done <- d.ActivateAndServe() }(d)
	}
	select {
	case e = <-done:
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, d := range ds {
		_ = d.ShutdownContext(stop)
	}
	if se := s.Shutdown(stop); se != nil {
		_ = s.Close()
		if e == nil {
			e = se
		}
	}
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
