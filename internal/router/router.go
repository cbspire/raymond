// Package router implements immutable longest-label-suffix routing.
package router

import (
	"github.com/cbspire/raymond/internal/config"
	"sort"
)

type Router struct{ routes []config.Installation }

// DashboardHost is the built-in dashboard landing page hostname, used only without a matching route.
const DashboardHost = "raymond.test"

func New(routes []config.Installation) *Router {
	r := &Router{routes: append([]config.Installation(nil), routes...)}
	sort.Slice(r.routes, func(i, j int) bool { return len(r.routes[i].Domain) > len(r.routes[j].Domain) })
	return r
}
func (r *Router) Match(host string) (string, bool) {
	h, e := config.Normalize(host)
	if e != nil {
		return "", false
	}
	for _, v := range r.routes {
		if config.Within(h, v.Domain) {
			return v.Target, true
		}
	}
	return "", false
}
