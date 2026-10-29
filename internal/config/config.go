// Package config loads Raymond's versioned configuration without guessing intent.
package config

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type DNS struct {
	Zones  []string `yaml:"zones"`
	Listen string   `yaml:"listen"`
}
type Proxy struct {
	Listen string `yaml:"listen"`
}
type Installation struct {
	Domain string `yaml:"domain"`
	Target string `yaml:"target"`
}
type Config struct {
	Version       int            `yaml:"version"`
	DNS           DNS            `yaml:"dns"`
	Proxy         Proxy          `yaml:"proxy"`
	Installations []Installation `yaml:"installations"`
}

// Normalize uses ASCII DNS labels; Unicode must be supplied as explicit punycode.
func Normalize(s string) (string, error) {
	for _, b := range []byte(s) {
		if b >= 128 {
			return "", fmt.Errorf("hostname must use ASCII DNS labels")
		}
	}
	s = strings.ToLower(strings.TrimSuffix(s, "."))
	if len(s) == 0 || len(s) > 253 {
		return "", fmt.Errorf("invalid hostname length")
	}
	for _, l := range strings.Split(s, ".") {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", fmt.Errorf("invalid DNS label")
		}
		for _, c := range l {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", fmt.Errorf("hostname must use ASCII DNS labels")
			}
		}
	}
	return s, nil
}
func Within(host, suffix string) bool { return host == suffix || strings.HasSuffix(host, "."+suffix) }
func Load(path string) (Config, error) {
	f, e := os.Open(path)
	if e != nil {
		return Config{}, e
	}
	defer f.Close()
	return Decode(f)
}
func Decode(r io.Reader) (Config, error) {
	c := Config{}
	d := yaml.NewDecoder(io.LimitReader(r, 1<<20))
	d.KnownFields(true)
	if e := d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return c, fmt.Errorf("expected exactly one YAML document")
	}
	return c, c.Validate()
}
func address(s string) (netip.Addr, error) {
	h, p, e := net.SplitHostPort(s)
	if e != nil {
		return netip.Addr{}, e
	}
	n, e := strconv.Atoi(p)
	if e != nil || n < 1 || n > 65535 || p != strconv.Itoa(n) {
		return netip.Addr{}, fmt.Errorf("invalid port")
	}
	a, e := netip.ParseAddr(h)
	if e != nil || !a.Is4() || !a.IsLoopback() {
		return a, fmt.Errorf("address must be an IPv4 loopback literal")
	}
	return a, nil
}
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if c.DNS.Listen == "" {
		c.DNS.Listen = "127.0.0.43:53"
	}
	if c.Proxy.Listen == "" {
		c.Proxy.Listen = "127.0.0.42:80"
	}
	da, e := address(c.DNS.Listen)
	if e != nil {
		return fmt.Errorf("dns.listen: %w", e)
	}
	pa, e := address(c.Proxy.Listen)
	if e != nil {
		return fmt.Errorf("proxy.listen: %w", e)
	}
	if da == pa || pa.String() == "127.0.0.1" || da.String() == "127.0.0.1" {
		return fmt.Errorf("DNS and proxy require separate dedicated loopback addresses")
	}
	for _, a := range []netip.Addr{da, pa} {
		if a.String() == "127.0.0.53" || a.String() == "127.0.0.54" {
			return fmt.Errorf("reserved systemd-resolved address")
		}
	}
	if len(c.DNS.Zones) == 0 {
		return fmt.Errorf("at least one DNS zone required")
	}
	seen := map[string]bool{}
	for i, z := range c.DNS.Zones {
		z, e = Normalize(z)
		if e != nil {
			return fmt.Errorf("zone: %w", e)
		}
		if Within(z, "local") || seen[z] {
			return fmt.Errorf("duplicate zone or forbidden .local zone")
		}
		seen[z] = true
		c.DNS.Zones[i] = z
	}
	seen = map[string]bool{}
	for i, r := range c.Installations {
		r.Domain, e = Normalize(r.Domain)
		if e != nil {
			return fmt.Errorf("installation domain: %w", e)
		}
		if seen[r.Domain] {
			return fmt.Errorf("duplicate installation %s", r.Domain)
		}
		seen[r.Domain] = true
		found := false
		for _, z := range c.DNS.Zones {
			found = found || Within(r.Domain, z)
		}
		if !found {
			return fmt.Errorf("installation %s outside configured zones", r.Domain)
		}
		if _, e = address(r.Target); e != nil {
			return fmt.Errorf("target for %s: %w", r.Domain, e)
		}
		if r.Target == c.Proxy.Listen || r.Target == c.DNS.Listen {
			return fmt.Errorf("installation target must not point to Raymond listeners")
		}
		c.Installations[i] = r
	}
	return nil
}
