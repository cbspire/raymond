package diagnostics

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cbspire/raymond/internal/config"
)

func TestWildcardPreflightDoesNotNeedLoopbackAliases(t *testing.T) {
	listener, e := net.Listen("tcp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	c := config.Config{Proxy: config.Proxy{Listen: "127.0.0.42:" + port}, DNS: config.DNS{Listen: "127.0.0.43:53535"}}
	if e := CheckWildcardListeners(c); e == nil || !strings.Contains(e.Error(), "wildcard TCP listener") {
		t.Fatalf("expected conflict before alias setup, got %v", e)
	}
}

func TestWildcardCoexistenceOptInKeepsOtherAddressesWorking(t *testing.T) {
	wildcard, e := net.Listen("tcp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	fallback := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "existing service") }), ReadHeaderTimeout: time.Second}
	go fallback.Serve(wildcard)
	defer fallback.Close()
	port := strconv.Itoa(wildcard.Addr().(*net.TCPAddr).Port)
	proxyAddr := "127.0.0.1:" + port
	c := config.Config{Proxy: config.Proxy{Listen: proxyAddr, AllowWildcardCoexistence: true}, DNS: config.DNS{Listen: "127.0.0.1:53535"}}
	if e := CheckWildcardListeners(c); e != nil {
		t.Fatalf("opt-in rejected compatible wildcard: %v", e)
	}
	listener, e := net.Listen("tcp4", proxyAddr)
	if e != nil {
		t.Fatalf("cannot bind exact address: %v", e)
	}
	local := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "Raymond") }), ReadHeaderTimeout: time.Second}
	go local.Serve(listener)
	defer local.Close()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, e := client.Get("http://" + proxyAddr)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	b, e := io.ReadAll(response.Body)
	if e != nil || string(b) != "Raymond" {
		t.Fatalf("specific listener lost traffic: %q, %v", b, e)
	}
	// All-address fallback still accepts a separate local IP when one is present.
	interfaces, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	for _, iface := range interfaces {
		addresses, e := iface.Addrs()
		if e != nil {
			t.Fatal(e)
		}
		for _, address := range addresses {
			ip, _, e := net.ParseCIDR(address.String())
			if e != nil || ip.To4() == nil || ip.IsUnspecified() || ip.String() == "127.0.0.1" {
				continue
			}
			response, e := client.Get("http://" + net.JoinHostPort(ip.String(), port))
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(response.Body)
			response.Body.Close()
			if e != nil || string(b) != "existing service" {
				t.Fatalf("fallback listener changed: %q, %v", b, e)
			}
			return
		}
	}
	t.Log("no second IPv4 address available; exact-address response verified")
}

func TestWildcardCoexistenceDoesNotBypassDNSConflict(t *testing.T) {
	wildcard, e := net.Listen("tcp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	defer wildcard.Close()
	port := strconv.Itoa(wildcard.Addr().(*net.TCPAddr).Port)
	c := config.Config{Proxy: config.Proxy{Listen: "127.0.0.1:8080", AllowWildcardCoexistence: true}, DNS: config.DNS{Listen: "127.0.0.1:" + port}}
	if e := CheckWildcardListeners(c); e == nil {
		t.Fatal("HTTP workaround bypassed DNS conflict")
	}
}

func TestWildcardCoexistenceStillRejectsOccupiedExactAddress(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	c := config.Config{Proxy: config.Proxy{Listen: listener.Addr().String(), AllowWildcardCoexistence: true}, DNS: config.DNS{Listen: "127.0.0.1:53535"}}
	if e := Check(c); e == nil {
		t.Fatal("workaround bypassed an occupied exact address")
	}
}
