// Package router implements immutable longest-label-suffix routing.
package router

import (
	"github.com/cbspire/raymond/internal/config"
	"sort"
	"strings"
)

type Router struct{ routes []config.Route }

// DashboardHost is the built-in dashboard landing page hostname, used only without a matching route.
const DashboardHost = "raymond.test"

func New(routes []config.Route) *Router {
	r := &Router{routes: append([]config.Route(nil), routes...)}
	sort.Slice(r.routes, func(i, j int) bool {
		a, b := strings.TrimPrefix(r.routes[i].Domain, "*."), strings.TrimPrefix(r.routes[j].Domain, "*.")
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return strings.HasPrefix(r.routes[i].Domain, "*.") && !strings.HasPrefix(r.routes[j].Domain, "*.")
	})
	return r
}
func (r *Router) Match(host string) (string, bool) {
	h, e := config.Normalize(host)
	if e != nil {
		return "", false
	}
	for _, v := range r.routes {
		suffix := strings.TrimPrefix(v.Domain, "*.")
		if config.Within(h, suffix) && (suffix == v.Domain || h != suffix) {
			return v.Target, true
		}
	}
	return "", false
}
