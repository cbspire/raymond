package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)

type releaseClient interface {
	Do(*http.Request) (*http.Response, error)
}

func releaseInstaller(ctx context.Context, client releaseClient, tag string) ([]byte, string, error) {
	if tag == "" {
		req, e := http.NewRequestWithContext(ctx, http.MethodHead, "https://github.com/cbspire/raymond/releases/latest", nil)
		if e != nil {
			return nil, "", e
		}
		res, e := client.Do(req)
		if e != nil {
			return nil, "", e
		}
		res.Body.Close()
		const prefix = "/cbspire/raymond/releases/tag/"
		if res.StatusCode != http.StatusOK || res.Request.URL.Scheme != "https" || res.Request.URL.Host != "github.com" || !strings.HasPrefix(res.Request.URL.Path, prefix) {
			return nil, "", fmt.Errorf("could not resolve the latest Raymond release")
		}
		tag = strings.TrimPrefix(res.Request.URL.Path, prefix)
	}
	if !releaseVersion.MatchString(tag) {
		return nil, "", fmt.Errorf("expected a release version such as 0.1.1")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://raw.githubusercontent.com/cbspire/raymond/"+tag+"/scripts/install.sh", nil)
	if e != nil {
		return nil, "", e
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, "", e
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download release installer: HTTP %d", res.StatusCode)
	}
	const limit = 1 << 20
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return nil, "", e
	}
	if len(b) == 0 || len(b) > limit {
		return nil, "", fmt.Errorf("invalid release installer size")
	}
	return b, tag, nil
}

func upgradeCommand(args []string) error {
	tag := ""
	if len(args) == 2 && args[0] == "--version" {
		tag = args[1]
		if !releaseVersion.MatchString(tag) {
			return fmt.Errorf("invalid release version")
		}
	} else if len(args) != 0 {
		return fmt.Errorf("usage: raymond upgrade [--version 0.1.1]")
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("run raymond upgrade without sudo; setup requests administrator access when needed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	script, tag, e := releaseInstaller(ctx, &http.Client{Timeout: 30 * time.Second}, tag)
	if e != nil {
		return e
	}
	fmt.Printf("Installing Raymond %s...\n", tag)
	cmd := exec.Command("bash", "-s", "--")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "RAYMOND_VERSION="+tag)
	return cmd.Run()
}
