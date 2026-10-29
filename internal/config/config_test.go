package config

import (
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	c, e := Decode(strings.NewReader("version: 1\ndns:\n  zones: [test]\ninstallations:\n  - domain: INSTALL.WEB.TEST.\n    target: 127.0.0.1:9001\n"))
	if e != nil {
		t.Fatal(e)
	}
	if c.Proxy.Listen != "127.0.0.42:80" || c.Installations[0].Domain != "install.web.test" {
		t.Fatal(c)
	}
}
func TestUnknownKey(t *testing.T) {
	if _, e := Decode(strings.NewReader("version: 1\ntypo: true\n")); e == nil {
		t.Fatal("accepted unknown key")
	}
}
