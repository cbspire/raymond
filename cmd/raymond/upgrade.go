package main

import (
	"fmt"
	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/release"
	"os"
)

func upgradeVersion(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 2 && args[0] == "--version" && release.ValidVersion(args[1]) {
		return args[1], nil
	}
	return "", fmt.Errorf("usage: raymond upgrade [--version 0.1.2]")
}
func upgradeCommand(args []string) error {
	target, e := upgradeVersion(args)
	if e != nil {
		return e
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("run raymond upgrade as the enrolled owner, without sudo")
	}
	client, e := control.OwnerClient()
	if e != nil {
		return fmt.Errorf("run raymond setup once to enroll native verified upgrades: %w", e)
	}
	return managedCommand(client, control.Request{Action: "update", Version: target})
}
