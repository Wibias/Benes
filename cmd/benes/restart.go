package main

import (
	"context"
	"fmt"
	"io"
)

func runRestart(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: restart does not accept arguments")
		return 2
	}
	if code := runStop(stdout, stderr, deps); code != 0 {
		return code
	}
	return runServe(ctx, nil, stderr, deps)
}
