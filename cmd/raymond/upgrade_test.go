package main

import "testing"

func TestNativeUpgradeVersionArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "0.1.2"}, {"--version", "1.2.3"}} {
		if _, e := upgradeVersion(args); e != nil {
			t.Fatal(e)
		}
	}
	for _, args := range [][]string{{"--version"}, {"--version", "main"}, {"--version", "https://example.com/a"}, {"--version", "0.1.2-rc.1"}, {"--version", "0.1.2", "extra"}, {"--url", "x"}} {
		if _, e := upgradeVersion(args); e == nil {
			t.Fatal("accepted", args)
		}
	}
}
