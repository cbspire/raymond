package router

import (
	"github.com/cbspire/raymond/internal/config"
	"testing"
)

func TestMatch(t *testing.T) {
	r := New([]config.Installation{{Domain: "web.test", Target: "a"}, {Domain: "install.web.test", Target: "b"}})
	for _, v := range []struct{ host, want string }{{"install.web.test", "b"}, {"DEEP.site.install.web.test.", "b"}, {"other.web.test", "a"}, {"evilweb.test", ""}, {"unknown.internal", ""}} {
		got, _ := r.Match(v.host)
		if got != v.want {
			t.Errorf("%s: %s != %s", v.host, got, v.want)
		}
	}
}
