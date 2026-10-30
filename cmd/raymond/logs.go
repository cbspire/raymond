package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
)

func parseLogOptions(args []string) (install.LogOptions, error) {
	options := install.LogOptions{}
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&options.Lines, "lines", 50, "number of recent lines")
	flags.IntVar(&options.Lines, "n", 50, "number of recent lines")
	flags.BoolVar(&options.Follow, "follow", true, "follow new requests")
	flags.BoolVar(&options.Follow, "f", true, "follow new requests")
	noFollow := flags.Bool("no-follow", false, "print existing lines and exit")
	if e := flags.Parse(args); e != nil {
		return options, e
	}
	if len(flags.Args()) != 0 {
		return options, fmt.Errorf("logs takes no positional arguments")
	}
	if *noFollow {
		options.Follow = false
	}
	return options, options.Validate()
}

func logsCommand(args []string) error {
	options, e := parseLogOptions(args)
	if errors.Is(e, flag.ErrHelp) {
		fmt.Println("usage: raymond logs [--lines N | -n N] [--no-follow] [--follow | -f]\nShows the last 50 service log lines and follows new requests by default. Press Ctrl+C to stop.")
		return nil
	}
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if os.Geteuid() != 0 {
		if client, err := control.OwnerClient(); err == nil {
			return managedLogs(ctx, client, options)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return install.Logs(ctx, options)
}
