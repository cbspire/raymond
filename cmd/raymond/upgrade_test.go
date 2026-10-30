package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type releaseStub struct {
	urls   []string
	latest string
	status int
	script string
}

func (s *releaseStub) Do(r *http.Request) (*http.Response, error) {
	s.urls = append(s.urls, r.URL.String())
	final := r.Clone(r.Context())
	body := s.script
	if r.Method == http.MethodHead {
		final.URL, _ = url.Parse(s.latest)
		body = ""
	}
	return &http.Response{StatusCode: s.status, Body: io.NopCloser(strings.NewReader(body)), Request: final}, nil
}
func TestUpgradePinsInstallerToResolvedRelease(t *testing.T) {
	s := &releaseStub{latest: "https://github.com/cbspire/raymond/releases/tag/0.1.1", status: 200, script: "#!/bin/bash\n"}
	b, tag, e := releaseInstaller(context.Background(), s, "")
	if e != nil || tag != "0.1.1" || string(b) != s.script {
		t.Fatalf("%q %q %v", b, tag, e)
	}
	if len(s.urls) != 2 || s.urls[1] != "https://raw.githubusercontent.com/cbspire/raymond/0.1.1/scripts/install.sh" {
		t.Fatal(s.urls)
	}
}
func TestUpgradeRejectsBadReleaseAndDownload(t *testing.T) {
	for _, latest := range []string{"http://github.com/cbspire/raymond/releases/tag/0.1.1", "https://example.com/cbspire/raymond/releases/tag/0.1.1", "https://github.com/cbspire/raymond/releases/tag/main", "https://github.com/cbspire/raymond/releases/tag/0.1.1/extra"} {
		s := &releaseStub{latest: latest, status: 200, script: "script"}
		if _, _, e := releaseInstaller(context.Background(), s, ""); e == nil || len(s.urls) != 1 {
			t.Fatalf("accepted %s", latest)
		}
	}
	for _, s := range []*releaseStub{{status: 404, script: "missing"}, {status: 200}, {status: 200, script: strings.Repeat("x", (1<<20)+1)}} {
		if _, _, e := releaseInstaller(context.Background(), s, "0.1.1"); e == nil {
			t.Fatal("accepted invalid installer")
		}
	}
}
