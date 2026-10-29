package darwin

import (
	"encoding/xml"
	"github.com/cbspire/raymond/internal/config"
	"strings"
	"testing"
)

func TestResources(t *testing.T) {
	f := Files(config.Config{DNS: config.DNS{Zones: []string{"test", "internal"}}})
	for _, z := range []string{"test", "internal"} {
		s := string(f["/etc/resolver/"+z])
		if !strings.HasPrefix(s, Marker) || !strings.Contains(s, "127.0.0.43") {
			t.Fatal(s)
		}
	}
	var v any
	if e := xml.Unmarshal(f["/Library/LaunchDaemons/dev.raymond.plist"], &v); e != nil {
		t.Fatal(e)
	}
}
