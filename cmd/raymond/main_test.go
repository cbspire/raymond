package main

import (
	"testing"
)

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"version", "extra"}, {"config"}, {"config", "validate", "--bad", "file"}, {"serve", "--config"}, {"start", "extra"}, {"uninstall", "extra"}} {
		if e := run(args); e == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
func TestVersionAndConfigPath(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"config", "path"}} {
		if e := run(args); e != nil {
			t.Fatal(e)
		}
	}
}
