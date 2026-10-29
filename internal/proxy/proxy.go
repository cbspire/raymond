// Package proxy provides HTTP-only, dynamically routed loopback proxying.
package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"github.com/cbspire/raymond/internal/router"
)

type targetKey struct{}

func New(routes *router.Router) http.Handler {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConns: 100, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DisableCompression: true}
	p := &httputil.ReverseProxy{Transport: transport, FlushInterval: -1, Rewrite: func(r *httputil.ProxyRequest) {
		r.Out.URL.Scheme = "http"
		r.Out.URL.Host = r.In.Context().Value(targetKey{}).(string)
		r.Out.Host = r.In.Host
		r.Out.Header.Del("Forwarded")
		r.SetXForwarded()
		r.Out.Header.Set("X-Forwarded-Proto", "http")
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Raymond: backend unavailable", http.StatusBadGateway)
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			http.Error(w, "Raymond: CONNECT is unsupported", http.StatusMethodNotAllowed)
			return
		}
		host := r.Host
		if strings.Contains(host, ":") {
			h, port, e := net.SplitHostPort(host)
			n, pe := strconv.Atoi(port)
			if e != nil || pe != nil || n < 1 || n > 65535 {
				http.Error(w, "Raymond: invalid Host", http.StatusBadRequest)
				return
			}
			host = h
		}
		target, ok := routes.Match(host)
		if !ok {
			http.Error(w, "Raymond: no installation for hostname", http.StatusNotFound)
			return
		}
		p.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), targetKey{}, target)))
	})
}
