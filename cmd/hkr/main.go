// Command hkr is the command-line client for HKr. It only wires the real
// environment into internal/cli and exits with the code it returns.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/chrom-ar/hkr-cli/internal/cli"
)

// Stamped at build time with -ldflags "-X main.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return cli.Run(ctx, args, cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Getenv: os.Getenv,
		Build:  cli.Build{Version: version, Commit: commit, Date: date},
	})
}
