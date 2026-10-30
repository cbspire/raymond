package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/cbspire/raymond/internal/install"
)

func setupCommand(args []string) error {
	path, e := userFileArg(args, "--config")
	if e != nil {
		return e
	}
	if len(args) == 0 {
		path, _, e = initializeUserConfig()
		if e != nil {
			return e
		}
	}
	if os.Geteuid() != 0 {
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		fmt.Println("Raymond needs administrator access to configure local DNS and start its service.")
		cmd := exec.Command("/usr/bin/sudo", "--", exe, "setup", "--config", path)
		tty, e := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if e == nil {
			defer tty.Close()
			cmd.Stdin = tty
		} else {
			cmd.Stdin = os.Stdin
		}
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	if e = install.Setup(path); e != nil {
		return e
	}
	fmt.Println("Raymond is ready at http://raymond.test. Use 'raymond logs' to follow requests.")
	return nil
}
