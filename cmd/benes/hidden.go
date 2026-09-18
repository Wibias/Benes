package main

import (
	"context"
	"fmt"
	"io"
)

func runHidden(ctx context.Context, command string, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "benes: %s does not accept arguments\n", command)
		return 2
	}
	switch command {
	case "__refresh-version":
		return 0
	case "__gui-update-worker":
		fmt.Fprintln(stdout, "Benes does not self-update in-process.")
		return 0
	case "__tray-start":
		return runEnsure(ctx, nil, stdout, stderr, deps)
	case "__tray-restart":
		return runRestart(ctx, nil, stdout, stderr, deps)
	case "__startup-health":
		return writeStartupHealth(stdout, deps)
	case "__tray-host":
		return runTrayHost(stdout, stderr)

	default:
		fmt.Fprintf(stderr, "benes: unknown command %q\n", command)
		return 2
	}
}
