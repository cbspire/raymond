package proxy

import (
	"bufio"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/router"
	"golang.org/x/net/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, h http.HandlerFunc) (*httptest.Server, *httptest.Server) {
	t.Helper()
	b := httptest.NewServer(h)
	p := httptest.NewServer(New(router.New([]config.Route{{Domain: "install.test", Target: strings.TrimPrefix(b.URL, "http://")}})))
	t.Cleanup(b.Close)
	t.Cleanup(p.Close)
	return b, p
}
func TestPreservation(t *testing.T) {
	body := strings.Repeat("multipart payload\x00", 100000)
	_, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "DEEP.install.test:8080" || r.Method != "POST" || r.URL.RequestURI() != "/a%2Fb/?x=1&x=2" {
			t.Errorf("request changed: %s %s %s", r.Host, r.Method, r.URL.RequestURI())
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != body {
			t.Error("body changed")
		}
		if r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "127.0.0.1" || r.Header.Get("X-Forwarded-Host") != r.Host || r.Header.Get("X-Forwarded-Proto") != "http" {
			t.Errorf("forwarded headers: %v", r.Header)
		}
		w.Header().Add("Set-Cookie", "session=one; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "other=two; Path=/")
		w.Header().Set("Location", "http://DEEP.install.test/admin/")
		w.WriteHeader(302)
	})
	req, _ := http.NewRequest("POST", p.URL+"/a%2Fb/?x=1&x=2", strings.NewReader(body))
	req.Host = "DEEP.install.test:8080"
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		req.Header.Set(h, "untrusted")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 || len(resp.Header.Values("Set-Cookie")) != 2 || resp.Header.Get("Location") != "http://DEEP.install.test/admin/" {
		t.Fatal(resp.Status, resp.Header)
	}
}
func TestErrors(t *testing.T) {
	_, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	req, _ := http.NewRequest("GET", p.URL, nil)
	req.Host = "unknown.test"
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal(resp.Status)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	l.Close()
	s := httptest.NewServer(New(router.New([]config.Route{{Domain: "install.test", Target: addr}})))
	defer s.Close()
	req, _ = http.NewRequest("GET", s.URL, nil)
	req.Host = "install.test"
	resp, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Fatal(resp.Status)
	}
}
func TestStreaming(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	_, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "first\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "last\n")
	})
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("GET", p.URL, nil)
	req.Host = "install.test"
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	line, e := bufio.NewReader(resp.Body).ReadString('\n')
	if e != nil || line != "first\n" {
		t.Fatal(line, e)
	}
}
func TestUpgrade(t *testing.T) {
	_, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		c, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer c.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		io.Copy(c, rw)
	})
	c, e := net.Dial("tcp", strings.TrimPrefix(p.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: install.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	b := bufio.NewReader(c)
	req, _ := http.NewRequest("GET", p.URL, nil)
	resp, e := http.ReadResponse(b, req)
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 101 {
		t.Fatal(resp.Status)
	}
	fmt.Fprint(c, "upgrade payload\n")
	s, e := b.ReadString('\n')
	if e != nil || s != "upgrade payload\n" {
		t.Fatal(s, e)
	}
}
func TestConnectionReuse(t *testing.T) {
	addrs := make(chan string, 2)
	_, p := setup(t, func(w http.ResponseWriter, r *http.Request) { addrs <- r.RemoteAddr; fmt.Fprint(w, "ok") })
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", p.URL, nil)
		req.Host = "install.test"
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if a, b := <-addrs, <-addrs; a != b {
		t.Fatalf("backend connections not reused: %s %s", a, b)
	}
}

func TestWebSocketFrames(t *testing.T) {
	_, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		websocket.Handler(func(ws *websocket.Conn) {
			defer ws.Close()
			var payload string
			if e := websocket.Message.Receive(ws, &payload); e != nil {
				return
			}
			_ = websocket.Message.Send(ws, payload)
		}).ServeHTTP(w, r)
	})
	cfg, e := websocket.NewConfig("ws://install.test/socket", "http://install.test")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := net.DialTimeout("tcp", strings.TrimPrefix(p.URL, "http://"), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	ws, e := websocket.NewClient(cfg, conn)
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	if e = websocket.Message.Send(ws, "application event stream"); e != nil {
		t.Fatal(e)
	}
	var got string
	if e = websocket.Message.Receive(ws, &got); e != nil || got != "application event stream" {
		t.Fatal(got, e)
	}
}
