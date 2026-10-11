package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/MBFrosty/macjobs/internal/cli"
	"github.com/MBFrosty/macjobs/internal/redact"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ver := VersionString(resolveVersion(version), commit, date)
	if err := cli.New(ver, cli.Live).ExecuteContext(ctx); err != nil {
		if !errors.Is(err, cli.ErrUnhealthy) && !errors.Is(err, cli.ErrActionReported) {
			fmt.Fprintln(os.Stderr, redact.Redactor{}.Text(err.Error()))
		}
		os.Exit(1)
	}
}
