package router

import (
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"testing"
)

func TestMatch(t *testing.T) {
	r := New([]config.Route{{Domain: "web.test", Target: "a"}, {Domain: "install.web.test", Target: "b"}})
	for _, v := range []struct{ host, want string }{{"install.web.test", "b"}, {"DEEP.site.install.web.test.", "b"}, {"other.web.test", "a"}, {"evilweb.test", ""}, {"unknown.internal", ""}} {
		got, _ := r.Match(v.host)
		if got != v.want {
			t.Errorf("%s: %s != %s", v.host, got, v.want)
		}
	}
}

func TestFiftyHosts(t *testing.T) {
	routes := make([]config.Route, 10)
	for i := range routes {
		routes[i] = config.Route{Domain: fmt.Sprintf("install%02d.web.test", i), Target: fmt.Sprintf("127.0.0.1:%d", 9000+i)}
	}
	r := New(routes)
	for i := range routes {
		for j := 0; j < 5; j++ {
			host := fmt.Sprintf("preview.eu.site%d.%s", j, routes[i].Domain)
			got, ok := r.Match(host)
			if !ok || got != routes[i].Target {
				t.Fatalf("%s: %s", host, got)
			}
		}
	}
	routes[0].Target = "mutated"
	if got, _ := r.Match("install00.web.test"); got == "mutated" {
		t.Fatal("router retained mutable input")
	}
}
func FuzzLabelBoundaries(f *testing.F) {
	f.Add("site")
	f.Add("deep.site")
	f.Fuzz(func(t *testing.T, prefix string) {
		r := New([]config.Route{{Domain: "install.test", Target: "backend"}})
		host := prefix + ".install.test"
		if _, e := config.Normalize(host); e == nil {
			if _, ok := r.Match(host); !ok {
				t.Fatal(host)
			}
		}
		if _, ok := r.Match(prefix + "evilinstall.test"); ok {
			t.Fatal("matched lookalike suffix")
		}
	})
}

func TestExplicitWildcards(t *testing.T) {
	r := New([]config.Route{{Domain: "*.test", Target: "all"}, {Domain: "*.apps.test", Target: "apps"}, {Domain: "api.apps.test", Target: "specific"}})
	for host, want := range map[string]string{"test": "", "x.test": "all", "apps.test": "all", "x.apps.test": "apps", "deep.x.apps.test": "apps", "api.apps.test": "specific", "deep.api.apps.test": "specific", "evilapps.test": "all", "test.example": ""} {
		got, _ := r.Match(host)
		if got != want {
			t.Errorf("%s: got %q want %q", host, got, want)
		}
	}
}

func TestWildcardAndApexPrecedence(t *testing.T) {
	for _, routes := range [][]config.Route{
		{{Domain: "apps.test", Target: "apex"}, {Domain: "*.apps.test", Target: "children"}},
		{{Domain: "*.apps.test", Target: "children"}, {Domain: "apps.test", Target: "apex"}},
	} {
		r := New(routes)
		for host, want := range map[string]string{"apps.test": "apex", "one.apps.test": "children", "deep.one.apps.test": "children"} {
			got, _ := r.Match(host)
			if got != want {
				t.Fatalf("%s: got %s want %s", host, got, want)
			}
		}
	}
}
