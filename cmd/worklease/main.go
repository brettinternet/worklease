package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

	appcli "github.com/brettinternet/worklease/internal/cli"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
)

var (
	buildVersion           = "dev"
	buildCommit            = "unknown"
	buildTime              = "unknown"
	outputWriter io.Writer = os.Stdout
	errorWriter  io.Writer = os.Stderr
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args); err != nil {
		if !appcli.JSONErrorHandled(err) {
			_ = output.WriteTextError(errorWriter, err)
		}
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return reason.ExitInterrupted
	}
	var exitErr urfavecli.ExitCoder
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func run(ctx context.Context, args []string) error {
	return appcli.Run(ctx, args, buildVersion, buildCommit, buildTime, outputWriter, errorWriter)
}
