package main

import (
	"context"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/server"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"
var commit = "unknown"

func run(args []string) error {
	if len(args) == 3 && args[0] == "serve" && args[1] == "--config" {
		c, e := config.Load(args[2])
		if e != nil {
			return e
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return server.Run(ctx, c)
	}
	if len(args) == 1 && args[0] == "version" {
		fmt.Printf("raymond %s (%s)\n", version, commit)
		return nil
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "validate" {
		path := "/etc/raymond/config.yaml"
		if len(args) == 4 && args[2] == "--file" {
			path = args[3]
		} else if len(args) != 2 {
			return fmt.Errorf("usage: raymond config validate [--file path]")
		}
		_, e := config.Load(path)
		return e
	}
	return fmt.Errorf("usage: raymond version | config validate [--file path]")
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
