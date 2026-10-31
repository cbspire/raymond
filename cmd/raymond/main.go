package main

import (
	"context"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/diagnostics"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/manager"
	"github.com/cbspire/raymond/internal/server"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
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
func userFileArg(args []string, flag string) (string, error) {
	if len(args) == 0 {
		return config.UserPath()
	}
	if len(args) == 2 && args[0] == flag && args[1] != "" {
		return args[1], nil
	}
	return "", fmt.Errorf("expected [%s path]", flag)
}

func installCommand(args []string) error {
	p, e := userFileArg(args, "--config")
	if e != nil {
		return e
	}
	if os.Geteuid() != 0 {
		if client, err := control.OwnerClient(); err == nil {
			if client.Enrolled {
				if len(args) == 0 {
					p = ""
				}
				return setupManaged(client, p)
			}
			client.HTTP.CloseIdleConnections()
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if len(args) == 0 {
		var created bool
		p, created, e = initializeUserConfig()
		if e != nil {
			return e
		}
		if created {
			fmt.Printf("Created user configuration: %s\n", p)
		} else {
			fmt.Printf("Using existing user configuration: %s\n", p)
		}
	}
	if _, e = config.Load(p); e != nil {
		return e
	}
	if os.Geteuid() != 0 {
		fmt.Printf("User configuration ready: %s\n", p)
		fmt.Println("Edit your routes, then rerun install with sudo to configure DNS and the background service. See docs/installation.md.")
		return nil
	}
	return install.Install(p)
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: raymond version|config|doctor|install|setup|dashboard|upgrade|start|stop|restart|status|logs|uninstall|serve")
	}
	switch args[0] {
	case "_owner-io":
		if len(args) != 1 {
			break
		}
		return manager.OwnerHelper(os.Stdin, os.Stdout)
	case "_operation":
		if len(args) != 1 || os.Geteuid() != 0 {
			break
		}
		f := os.NewFile(3, "operations.lock")
		defer f.Close()
		return manager.Worker(version, f)
	case "_manager":
		if len(args) != 1 {
			break
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return manager.Run(ctx, version)
	case "dashboard":
		return dashboardCommand(args[1:])
	case "_init-user-config":
		if len(args) != 1 || os.Geteuid() == 0 {
			return fmt.Errorf("user config helper requires an ordinary user and no arguments")
		}
		_, created, e := config.InitUser()
		if e != nil {
			return e
		}
		fmt.Println(created)
		return nil
	case "version":
		if len(args) != 1 {
			break
		}
		fmt.Printf("raymond %s (%s)\n", version, commit)
		return nil
	case "config":
		if len(args) == 2 && args[1] == "path" {
			p, e := config.UserPath()
			if e != nil {
				return e
			}
			fmt.Println(p)
			return nil
		}
		if len(args) >= 2 && args[1] == "validate" {
			p, e := userFileArg(args[2:], "--file")
			if e != nil {
				return e
			}
			_, e = config.Load(p)
			return e
		}
	case "serve":
		p, e := userFileArg(args[1:], "--config")
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
		p, e := userFileArg(args[1:], "--config")
		if e != nil {
			return e
		}
		if os.Geteuid() != 0 {
			if client, err := control.OwnerClient(); err == nil {
				if len(args) > 1 {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					var state control.State
					err = client.Call(ctx, "GET", control.Prefix+"state", nil, &state)
					cancel()
					if err != nil {
						return err
					}
					absolute, e := filepath.Abs(p)
					if e != nil {
						return e
					}
					if absolute != state.ConfigPath {
						return fmt.Errorf("doctor config must match the enrolled configuration")
					}
				}
				return managedCommand(client, control.Request{Action: "diagnostics"})
			} else if !os.IsNotExist(err) {
				return err
			}
		}

		c, e := config.Load(p)
		if e != nil {
			return e
		}
		if e = diagnostics.Check(c); e != nil {
			return e
		}
		if e = install.PlatformCheck(c.DNS.Zones...); e != nil {
			return e
		}
		fmt.Println("Loopback TCP/UDP bind checks passed. This does not validate OS split DNS. If ports are privileged, run doctor explicitly with sudo.")
		return nil
	case "install":
		return installCommand(args[1:])
	case "setup":
		return setupCommand(args[1:])
	case "upgrade", "update":
		return upgradeCommand(args[1:])
	case "logs":
		return logsCommand(args[1:])
	case "start", "stop", "restart", "status":
		if len(args) != 1 {
			break
		}
		if os.Geteuid() != 0 {
			if client, err := control.OwnerClient(); err == nil {
				if args[0] == "status" {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					defer client.HTTP.CloseIdleConnections()
					var state control.State
					if e := client.Call(ctx, "GET", control.Prefix+"state", nil, &state); e != nil {
						return e
					}
					fmt.Printf("Raymond %s: running=%t pending=%t\n", state.Version, state.Running, state.Pending)
					return nil
				}
				return managedCommand(client, control.Request{Action: args[0]})
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		if e := install.Control(args[0]); e != nil {
			return e
		}
		switch args[0] {
		case "start":
			fmt.Println("Raymond service started. Use 'raymond logs' to follow requests.")
		case "stop":
			fmt.Println("Raymond service stopped.")
		case "restart":
			fmt.Println("Raymond service restarted. Use 'raymond logs' to follow requests.")
		}
		return nil
	case "uninstall":
		if len(args) != 1 {
			break
		}
		return uninstallCommand()
	}
	return fmt.Errorf("invalid command or arguments")
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
