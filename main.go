// Command ltop is a terminal activity monitor for llama.cpp servers.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/pefman/ltop/internal/config"
	"github.com/pefman/ltop/internal/ui"
)

func main() {
	once := flag.Bool("once", false, "print a single plain-text snapshot and exit")
	reconfigure := flag.Bool("reconfigure", false, "re-run endpoint discovery and save a new endpoint")
	flag.Usage = usage
	flag.Parse()

	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "ltop: Linux only")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *once, *reconfigure); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "ltop: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, once, reconfigure bool) error {
	cfg, err := loadOrSetup(ctx, reconfigure)
	if err != nil {
		return err
	}
	if once {
		return runOnce(ctx, os.Stdout, cfg.Endpoint)
	}
	return ui.Run(ctx, cfg)
}

func loadOrSetup(ctx context.Context, reconfigure bool) (config.Config, error) {
	if !reconfigure {
		cfg, err := config.Load()
		if err == nil {
			return cfg, nil
		}
		if !errors.Is(err, config.ErrNotFound) {
			return config.Config{}, err
		}
	}
	return config.Setup(ctx, os.Stdin, os.Stderr)
}

func usage() {
	fmt.Fprint(os.Stderr, `ltop - activity monitor for llama.cpp servers

Usage:
  ltop [flags]

Flags:
  -once          print a single plain-text snapshot and exit
  -reconfigure   re-run endpoint discovery and save a new endpoint

The endpoint is discovered on first run and stored in the config file; it is
not read from the environment.
`)
}
