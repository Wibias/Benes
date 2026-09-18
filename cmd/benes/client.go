package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/Wibias/Benes/internal/managedfs"
)

func runClient(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  benes client apply --home DIR --client NAME --artifact FILE --from PATH")
		fmt.Fprintln(stdout, "  benes client disable --home DIR")
		fmt.Fprintln(stdout, "  benes client enable --home DIR")
		fmt.Fprintln(stdout, "  benes client recover --home DIR")
		fmt.Fprintln(stdout, "  benes client restore --home DIR --generation N")
		fmt.Fprintln(stdout, "  benes client adopt --home DIR --client NAME")
		fmt.Fprintln(stdout, "  benes client status --home DIR")
		return 0
	}
	switch args[0] {
	case "apply":
		return runClientApply(ctx, args[1:], stderr)
	case "disable":
		return runClientHomeOp(ctx, args[1:], stderr, func(c *managedfs.Coordinator) error { return c.Disable(ctx) })
	case "enable":
		return runClientHomeOp(ctx, args[1:], stderr, func(c *managedfs.Coordinator) error { return c.Enable(ctx) })
	case "recover":
		return runClientHomeOp(ctx, args[1:], stderr, func(c *managedfs.Coordinator) error { return c.Recover(ctx) })
	case "restore":
		return runClientRestore(ctx, args[1:], stderr)
	case "adopt":
		return runClientAdopt(ctx, args[1:], stderr)
	case "status":
		return runClientStatus(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "benes client: unknown command %q\n", args[0])
		return 2
	}
}

func runClientHomeOp(ctx context.Context, args []string, stderr io.Writer, op func(*managedfs.Coordinator) error) int {
	home := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--home" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client: --home requires a directory")
				return 2
			}
			home = args[i+1]
			i++
			continue
		}
		fmt.Fprintf(stderr, "benes client: unknown flag %q\n", args[i])
		return 2
	}
	if home == "" {
		fmt.Fprintln(stderr, "benes client: --home is required")
		return 2
	}
	coord, err := managedfs.New(home)
	if err != nil {
		fmt.Fprintf(stderr, "benes client: %v\n", err)
		return 1
	}
	if err := op(coord); err != nil {
		fmt.Fprintf(stderr, "benes client: %v\n", err)
		return 1
	}
	return 0
}

func runClientApply(ctx context.Context, args []string, stderr io.Writer) int {
	home, client, artifact, from := "", "", "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--home":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client apply: --home requires a directory")
				return 2
			}
			i++
			home = args[i]
		case "--client":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client apply: --client requires a name")
				return 2
			}
			i++
			client = args[i]
		case "--artifact":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client apply: --artifact requires a file name")
				return 2
			}
			i++
			artifact = args[i]
		case "--from":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client apply: --from requires a path")
				return 2
			}
			i++
			from = args[i]
		default:
			fmt.Fprintf(stderr, "benes client apply: unknown flag %q\n", args[i])
			return 2
		}
	}
	if home == "" || client == "" || artifact == "" || from == "" {
		fmt.Fprintln(stderr, "benes client apply: --home, --client, --artifact, and --from are required")
		return 2
	}
	if client == "codex" {
		if env := os.Getenv("CODEX_HOME"); env != "" {
			if err := managedfs.AssertSameHome(home, env); err != nil {
				fmt.Fprintf(stderr, "benes client apply: %v\n", err)
				return 1
			}
		}
	}
	body, err := os.ReadFile(from)
	if err != nil {
		fmt.Fprintf(stderr, "benes client apply: read source: %v\n", err)
		return 1
	}
	coord, err := managedfs.New(home)
	if err != nil {
		fmt.Fprintf(stderr, "benes client apply: %v\n", err)
		return 1
	}
	tx, err := coord.Begin(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "benes client apply: %v\n", err)
		return 1
	}
	if err := tx.Stage(artifact, body); err != nil {
		_ = tx.Rollback()
		fmt.Fprintf(stderr, "benes client apply: %v\n", err)
		return 1
	}
	if err := tx.Commit(); err != nil {
		fmt.Fprintf(stderr, "benes client apply: %v\n", err)
		return 1
	}
	return 0
}

func runClientStatus(args []string, stdout, stderr io.Writer) int {
	home := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--home" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client status: --home requires a directory")
				return 2
			}
			home = args[i+1]
			i++
			continue
		}
		fmt.Fprintf(stderr, "benes client status: unknown flag %q\n", args[i])
		return 2
	}
	if home == "" {
		fmt.Fprintln(stderr, "benes client status: --home is required")
		return 2
	}
	coord, err := managedfs.New(home)
	if err != nil {
		fmt.Fprintf(stderr, "benes client status: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, coord.Classify())
	return 0
}

func runClientAdopt(ctx context.Context, args []string, stderr io.Writer) int {
	home, client := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--home":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client adopt: --home requires a directory")
				return 2
			}
			i++
			home = args[i]
		case "--client":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client adopt: --client requires a name")
				return 2
			}
			i++
			client = args[i]
		default:
			fmt.Fprintf(stderr, "benes client adopt: unknown flag %q\n", args[i])
			return 2
		}
	}
	if home == "" || client == "" {
		fmt.Fprintln(stderr, "benes client adopt: --home and --client are required")
		return 2
	}
	coord, err := managedfs.New(home)
	if err != nil {
		fmt.Fprintf(stderr, "benes client adopt: %v\n", err)
		return 1
	}
	if err := coord.Adopt(ctx, client); err != nil {
		fmt.Fprintf(stderr, "benes client adopt: %v\n", err)
		return 1
	}
	return 0
}

func runClientRestore(ctx context.Context, args []string, stderr io.Writer) int {
	home, genRaw := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--home":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client restore: --home requires a directory")
				return 2
			}
			i++
			home = args[i]
		case "--generation":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes client restore: --generation requires a number")
				return 2
			}
			i++
			genRaw = args[i]
		default:
			fmt.Fprintf(stderr, "benes client restore: unknown flag %q\n", args[i])
			return 2
		}
	}
	if home == "" || genRaw == "" {
		fmt.Fprintln(stderr, "benes client restore: --home and --generation are required")
		return 2
	}
	generation, err := strconv.ParseInt(genRaw, 10, 64)
	if err != nil || generation < 1 {
		fmt.Fprintln(stderr, "benes client restore: --generation must be a positive integer")
		return 2
	}
	coord, err := managedfs.New(home)
	if err != nil {
		fmt.Fprintf(stderr, "benes client restore: %v\n", err)
		return 1
	}
	if err := coord.Restore(ctx, generation); err != nil {
		fmt.Fprintf(stderr, "benes client restore: %v\n", err)
		return 1
	}
	return 0
}
