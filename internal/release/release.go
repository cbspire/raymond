// Package release downloads immutable Raymond archives. Privileged upgrades use
// native verification and extraction, never a remote shell installer.
package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const Repo = "https://github.com/cbspire/raymond"

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func ValidVersion(v string) bool { return versionPattern.MatchString(v) }
func Newer(a, b string) bool {
	if !ValidVersion(a) {
		return false
	}
	if !ValidVersion(b) {
		return true
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		x, e := strconv.ParseUint(aa[i], 10, 64)
		if e != nil {
			return false
		}
		y, e := strconv.ParseUint(bb[i], 10, 64)
		if e != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}

type Client struct{ HTTP *http.Client }

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 || r.URL.Scheme != "https" {
			return fmt.Errorf("unsafe release redirect")
		}
		return nil
	}}}
}
func (c *Client) Latest(ctx context.Context) (string, error) {
	r, e := http.NewRequestWithContext(ctx, "HEAD", Repo+"/releases/latest", nil)
	if e != nil {
		return "", e
	}
	res, e := c.HTTP.Do(r)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	const path = "/cbspire/raymond/releases/tag/"
	if res.StatusCode != 200 || res.Request.URL.Scheme != "https" || res.Request.URL.Host != "github.com" || !strings.HasPrefix(res.Request.URL.Path, path) {
		return "", fmt.Errorf("could not resolve latest stable release")
	}
	v := strings.TrimPrefix(res.Request.URL.Path, path)
	if !ValidVersion(v) {
		return "", fmt.Errorf("invalid stable release version")
	}
	return v, nil
}
func (c *Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, e
	}
	res, e := c.HTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("release download HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e == nil && int64(len(b)) > limit {
		e = fmt.Errorf("release download too large")
	}
	return b, e
}
func (c *Client) Download(ctx context.Context, version, platform, arch, dir string) (string, error) {
	if !ValidVersion(version) {
		return "", fmt.Errorf("invalid stable release version")
	}
	if (platform != "darwin" && platform != "linux") || (arch != "amd64" && arch != "arm64") {
		return "", fmt.Errorf("unsupported release target")
	}
	name := "raymond_" + version + "_" + platform + "_" + arch + ".tar.gz"
	base := Repo + "/releases/download/" + version + "/"
	manifest, e := c.get(ctx, base+"checksums.txt", 64<<10)
	if e != nil {
		return "", e
	}
	archive, e := c.get(ctx, base+name, 128<<20)
	if e != nil {
		return "", e
	}
	if e = Verify(archive, manifest, name); e != nil {
		return "", e
	}
	binary, e := Extract(archive)
	if e != nil {
		return "", e
	}
	f, e := os.OpenFile(filepath.Join(dir, "raymond"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		return "", e
	}
	if _, e = f.Write(binary); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return f.Name(), e
}
func Verify(archive, manifest []byte, name string) error {
	expected := ""
	count := 0
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			expected = fields[0]
			count++
		}
	}
	h := sha256.Sum256(archive)
	if count != 1 || expected != hex.EncodeToString(h[:]) {
		return fmt.Errorf("missing, ambiguous or mismatched archive checksum")
	}
	return nil
}
func Extract(archive []byte) ([]byte, error) {
	zr, e := gzip.NewReader(bytes.NewReader(archive))
	if e != nil {
		return nil, e
	}
	defer zr.Close()
	r := tar.NewReader(io.LimitReader(zr, 260<<20))
	seen := map[string]bool{}
	var binary []byte
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if h.Typeflag != tar.TypeReg || seen[h.Name] || (h.Name != "raymond" && h.Name != "README.md" && h.Name != "LICENSE") {
			return nil, fmt.Errorf("unsafe release archive member")
		}
		seen[h.Name] = true
		limit := int64(1 << 20)
		if h.Name == "raymond" {
			limit = 256 << 20
		}
		if h.Size <= 0 || h.Size > limit {
			return nil, fmt.Errorf("invalid release member size")
		}
		b, e := io.ReadAll(io.LimitReader(r, limit+1))
		if e != nil {
			return nil, e
		}
		if h.Name == "raymond" {
			binary = b
		}
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("missing release executable")
	}
	return binary, nil
}
