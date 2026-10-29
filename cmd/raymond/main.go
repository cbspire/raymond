package main

import (
	"context"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/diagnostics"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/server"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"
var commit = "unknown"

func foreground(path string) error {
	c, e := config.Load(path)
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, c)
}
func fileArg(args []string, defaultPath, flag string) (string, error) {
	if len(args) == 0 {
		return defaultPath, nil
	}
	if len(args) == 2 && args[0] == flag && args[1] != "" {
		return args[1], nil
	}
	return "", fmt.Errorf("expected [%s path]", flag)
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: raymond version|config|doctor|install|start|stop|restart|status|uninstall|serve")
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			break
		}
		fmt.Printf("raymond %s (%s)\n", version, commit)
		return nil
	case "config":
		if len(args) == 2 && args[1] == "path" {
			fmt.Println(install.ConfigPath)
			return nil
		}
		if len(args) >= 2 && args[1] == "validate" {
			p, e := fileArg(args[2:], install.ConfigPath, "--file")
			if e != nil {
				return e
			}
			_, e = config.Load(p)
			return e
		}
	case "serve":
		p, e := fileArg(args[1:], install.ConfigPath, "--config")
		if e != nil {
			return e
		}
		return foreground(p)
	case "_service":
		if len(args) != 1 {
			break
		}
		if _, e := install.Installed(); e != nil {
			return e
		}
		if e := install.EnsureAliases(); e != nil {
			return e
		}
		return foreground(install.ConfigPath)
	case "doctor":
		p, e := fileArg(args[1:], install.ConfigPath, "--config")
		if e != nil {
			return e
		}
		c, e := config.Load(p)
		if e != nil {
			return e
		}
		if e = diagnostics.Check(c); e != nil {
			return e
		}
		if e = install.PlatformCheck(); e != nil {
			return e
		}
		fmt.Println("Loopback TCP/UDP bind checks passed. This does not validate OS split DNS. If ports are privileged, run doctor explicitly with sudo.")
		return nil
	case "install":
		p, e := fileArg(args[1:], install.ConfigPath, "--config")
		if e != nil {
			return e
		}
		return install.Install(p)
	case "start", "stop", "restart", "status":
		if len(args) != 1 {
			break
		}
		return install.Control(args[0])
	case "uninstall":
		if len(args) != 1 {
			break
		}
		return install.Uninstall()
	}
	return fmt.Errorf("invalid command or arguments")
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
