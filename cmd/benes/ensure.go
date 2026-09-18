package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

func runEnsure(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: ensure does not accept arguments")
		return 2
	}
	var statusOut bytes.Buffer
	if code := runStatus(nil, &statusOut, stderr, deps); code != 0 {
		return code

	}
	if !strings.Contains(statusOut.String(), "No running proxy found.") {
		fmt.Fprint(stdout, statusOut.String())
		return 0
	}
	return runServe(ctx, nil, stderr, deps)
}
