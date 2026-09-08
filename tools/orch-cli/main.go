package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/openark/orchestrator/tools/orch-cli/internal/cmd"
)

var AppVersion, GitCommit string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cmd.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, AppVersion, GitCommit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cmd.ExitCode(err))
	}
}
