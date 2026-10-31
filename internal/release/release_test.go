package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"testing"
)

func archive(t *testing.T, names []string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	w := tar.NewWriter(z)
	for _, name := range names {
		h := &tar.Header{Name: name, Size: 3, Mode: 0755, Typeflag: kind}
		if e := w.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if kind == tar.TypeReg {
			_, _ = w.Write([]byte("bin"))
		}
	}
	_ = w.Close()
	_ = z.Close()
	return b.Bytes()
}
func TestArchiveBoundary(t *testing.T) {
	good := archive(t, []string{"raymond", "README.md", "LICENSE"}, tar.TypeReg)
	if b, e := Extract(good); e != nil || string(b) != "bin" {
		t.Fatal(e)
	}
	name := "archive.tar.gz"
	sum := fmt.Sprintf("%x  %s\n", sha256.Sum256(good), name)
	if e := Verify(good, []byte(sum), name); e != nil {
		t.Fatal(e)
	}
	for _, manifest := range []string{"", sum + sum, "bad  " + name} {
		if e := Verify(good, []byte(manifest), name); e == nil {
			t.Fatal("accepted bad checksum")
		}
	}
	for _, names := range [][]string{{"../raymond"}, {"raymond", "raymond"}, {"README.md"}, {"raymond", "extra"}} {
		if _, e := Extract(archive(t, names, tar.TypeReg)); e == nil {
			t.Fatal("accepted unsafe archive", names)
		}
	}
	if _, e := Extract(archive(t, []string{"raymond"}, tar.TypeSymlink)); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestStableVersions(t *testing.T) {
	if !Newer("0.1.2", "0.1.1") || Newer("0.1.1", "0.1.2") || Newer("0.1.2", "0.1.2") {
		t.Fatal("bad comparison")
	}
	for _, s := range []string{"main", "v0.1.2", "0.1.2-rc.1", "../0.1.2", "00.1.2"} {
		if ValidVersion(s) {
			t.Fatal(s)
		}
	}
}
