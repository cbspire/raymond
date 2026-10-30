package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/manager"
)

func setupCommand(args []string) error {
	path, e := userFileArg(args, "--config")
	if e != nil {
		return e
	}
	if os.Geteuid() != 0 {
		if client, err := control.OwnerClient(); err == nil {
			if client.Enrolled {
				if len(args) == 0 {
					path = ""
				}
				return setupManaged(client, path)
			}
			client.HTTP.CloseIdleConnections()
		} else if !os.IsNotExist(err) {
			return err
		}
		if len(args) == 0 {
			path, _, e = initializeUserConfig()
			if e != nil {
				return e
			}
		}
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		fmt.Println("Raymond needs administrator access once to enroll local management, configure DNS and start its service.")
		command := exec.Command("/usr/bin/sudo", "--", executable, "setup", "--config", path)
		tty, e := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if e == nil {
			defer tty.Close()
			command.Stdin = tty
		} else {
			command.Stdin = os.Stdin
		}
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if e = command.Run(); e != nil {
			return e
		}
		if e = dashboardCommand(nil); e != nil {
			fmt.Printf("Raymond is ready. %v\n", e)
		}
		return nil
	}
	if install.ManagementAvailable() && install.ManagementReady() {
		client, e := rootManagedClient()
		if e != nil {
			return e
		}
		if e = install.StartManagement(false); e != nil {
			return e
		}
		if len(args) == 0 {
			path = ""
		}
		if e = setupManaged(client, path); e != nil {
			return e
		}
		metadata, e := install.ManagementMetadata()
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return manager.FinishEnrollment(ctx, metadata)
	}
	if len(args) == 0 {
		path, _, e = initializeUserConfig()
		if e != nil {
			return e
		}
	}
	if os.Getenv("SUDO_UID") != "" && os.Getenv("SUDO_UID") != "0" {
		if e = enrollSetup(path); e != nil {
			return e
		}
		fmt.Println("Raymond management enrolled. Future dashboard and CLI operations need no sudo.")
		return nil
	}
	// A deliberate root-only legacy installation has no ordinary account to enroll.
	if e = install.Setup(path); e != nil {
		return e
	}
	fmt.Println("Raymond service is ready. Run setup as the intended ordinary owner to enroll dashboard management.")
	return nil
}
