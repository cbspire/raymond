// Package server coordinates foreground serving and graceful termination.
package server

import (
	"context"
	"errors"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/proxy"
	"github.com/cbspire/raymond/internal/router"
	"net"
	"net/http"
	"time"
)

func Run(ctx context.Context, c config.Config) error {
	l, e := net.Listen("tcp4", c.Proxy.Listen)
	if e != nil {
		return e
	}
	defer l.Close()
	s := &http.Server{Handler: proxy.New(router.New(c.Installations)), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	select {
	case e = <-done:
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e = s.Shutdown(stop)
		if e != nil {
			_ = s.Close()
		}
		<-done
	}
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
