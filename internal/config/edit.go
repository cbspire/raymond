package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

func Revision(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// OwnedDocument is used by the unprivileged config helper, never by the root
// broker. Check both file identity and its directory before reading or replacing.
func OwnedDocument(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("invalid configuration path")
	}
	for p := filepath.Dir(path); p != "/"; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if runtime.GOOS == "darwin" && (p == "/var" || p == "/etc" || p == "/tmp") && st.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(p)
			if target == "private"+p || target == "/private"+p {
				continue
			}
		}
		if st.Mode()&os.ModeSymlink != 0 || (st.Mode().Perm()&0022 != 0 && st.Mode()&os.ModeSticky == 0) {
			return nil, fmt.Errorf("unsafe configuration ancestor %s", p)
		}
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	var identity unix.Stat_t
	if e = unix.Fstat(fd, &identity); e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || int(identity.Uid) != os.Geteuid() || st.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("config must be a regular file owned by the invoking user, not writable by others")
	}
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("configuration too large")
	}
	return b, nil
}
func SaveOwned(path, expected string, c Config) (string, error) {
	return saveOwned(path, expected, c, false)
}

// DiscardOwned is an explicit CAS restoration of the confirmed active config.
// Invalid external YAML has no parseable comments to merge.
func DiscardOwned(path, expected string, c Config) (string, error) {
	return saveOwned(path, expected, c, true)
}

func saveOwned(path, expected string, c Config, discard bool) (string, error) {
	if e := c.Validate(); e != nil {
		return "", e
	}
	_, e := OwnedDocument(path)
	if e != nil {
		return "", e
	}
	dir := filepath.Dir(path)
	lock, e := os.OpenFile(filepath.Join(dir, ".raymond-edit.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return "", e
	}
	defer lock.Close()
	lockInfo, e := lock.Stat()
	if e != nil {
		return "", e
	}
	var lockStat unix.Stat_t
	if e = unix.Fstat(int(lock.Fd()), &lockStat); e != nil {
		return "", e
	}
	if !lockInfo.Mode().IsRegular() || int(lockStat.Uid) != os.Geteuid() || lockInfo.Mode().Perm()&0077 != 0 || lockStat.Nlink != 1 {
		return "", fmt.Errorf("configuration edit lock must be private and owned")
	}
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return "", fmt.Errorf("another configuration edit is active")
	}
	old, e := OwnedDocument(path)
	if e != nil {
		return "", e
	}
	if Revision(old) != expected {
		return "", fmt.Errorf("configuration changed externally; reload before saving")
	}
	b, e := WithComments(old, c)
	if e != nil && discard {
		b, e = yaml.Marshal(c)
	}
	if e != nil {
		return "", e
	}
	f, e := os.CreateTemp(dir, ".raymond-config-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	// Recheck immediately before publication so ordinary external edits are not lost.
	current, e := OwnedDocument(path)
	if e != nil {
		return "", e
	}
	if Revision(current) != expected {
		return "", fmt.Errorf("configuration changed externally; reload before saving")
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return "", e
	}
	d, e := os.Open(dir)
	if e != nil {
		return "", e
	}
	defer d.Close()
	return Revision(b), d.Sync()
}

func WithComments(old []byte, c Config) ([]byte, error) {
	if _, e := Decode(bytes.NewReader(old)); e != nil {
		return nil, e
	}
	var before, after yaml.Node
	if e := yaml.Unmarshal(old, &before); e != nil {
		return nil, e
	}
	b, e := yaml.Marshal(c)
	if e != nil {
		return nil, e
	}
	if e = yaml.Unmarshal(b, &after); e != nil {
		return nil, e
	}
	normalizeRouteKeys(&before)
	used := map[string]bool{}
	var copyComments func(*yaml.Node, *yaml.Node)
	copyComments = func(a, b *yaml.Node) {
		b.HeadComment, b.LineComment, b.FootComment = a.HeadComment, a.LineComment, a.FootComment
		for _, s := range []string{a.HeadComment, a.LineComment, a.FootComment} {
			if s != "" {
				used[s] = true
			}
		}
		if a.Kind == yaml.MappingNode && b.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(b.Content); i += 2 {
				for j := 0; j+1 < len(a.Content); j += 2 {
					if a.Content[j].Value == b.Content[i].Value {
						copyComments(a.Content[j], b.Content[i])
						copyComments(a.Content[j+1], b.Content[i+1])
						break
					}
				}
			}
		} else if a.Kind == yaml.SequenceNode && b.Kind == yaml.SequenceNode {
			domain := func(n *yaml.Node) string {
				if n.Kind == yaml.MappingNode {
					for i := 0; i+1 < len(n.Content); i += 2 {
						if n.Content[i].Value == "domain" {
							return strings.ToLower(strings.TrimSuffix(n.Content[i+1].Value, "."))
						}
					}
				}
				return n.Value
			}
			for _, n := range b.Content {
				for _, o := range a.Content {
					if domain(n) == domain(o) {
						copyComments(o, n)
						break
					}
				}
			}
		} else if a.Kind == yaml.DocumentNode && b.Kind == yaml.DocumentNode && len(a.Content) > 0 {
			copyComments(a.Content[0], b.Content[0])
		}
	}
	copyComments(&before, &after)
	var collect func(*yaml.Node)
	collect = func(n *yaml.Node) {
		for _, s := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			if s != "" && !used[s] {
				after.Content[0].FootComment += "\n" + s
				used[s] = true
			}
		}
		for _, child := range n.Content {
			collect(child)
		}
	}
	collect(&before)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if e = encoder.Encode(&after); e != nil {
		return nil, e
	}
	return out.Bytes(), encoder.Close()
}
