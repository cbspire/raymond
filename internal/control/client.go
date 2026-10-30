package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cbspire/raymond/internal/config"
)

type Client struct {
	Token string
	HTTP  *http.Client
}

func NewClient(socket, token string) *Client {
	t := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	return &Client{Token: token, HTTP: &http.Client{Transport: t}}
}
func OwnerClient() (*Client, error) {
	p, e := config.UserPath()
	if e != nil {
		return nil, e
	}
	b, e := os.ReadFile(filepath.Join(filepath.Dir(p), "control.json"))
	if e != nil {
		return nil, e
	}
	var access struct {
		Token string `json:"token"`
	}
	if e = json.Unmarshal(b, &access); e != nil {
		return nil, e
	}
	if len(access.Token) < 32 {
		return nil, fmt.Errorf("invalid management credential; run raymond setup")
	}
	return NewClient(SocketPath, access.Token), nil
}
func ProxyClient() (*Client, error) {
	p := filepath.Join(RootDir, "proxy-token")
	if d := os.Getenv("CREDENTIALS_DIRECTORY"); d != "" {
		p = filepath.Join(d, "raymond-control")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, e
	}
	return NewClient(SocketPath, string(b)), nil
}
func (c *Client) Call(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		r = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://control"+path, r)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &Error{res.StatusCode, string(b)}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out)
	}
	return nil
}

// Forward exposes only the versioned browser API. Owner-only endpoints remain
// inaccessible even if a browser supplies its own Authorization header.
func (c *Client) Forward(w http.ResponseWriter, r *http.Request) {
	req := r.Clone(r.Context())
	req.URL.Scheme = "http"
	req.URL.Host = "control"
	req.RequestURI = ""
	req.Header = r.Header.Clone()
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, e := c.HTTP.Do(req)
	if e != nil {
		http.Error(w, "Management is unavailable; run raymond setup.", http.StatusServiceUnavailable)
		return
	}
	defer res.Body.Close()
	for k, vs := range res.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	buf := make([]byte, 16<<10)
	for {
		n, e := res.Body.Read(buf)
		if n > 0 {
			if _, we := w.Write(buf[:n]); we != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if e != nil {
			return
		}
	}
}
