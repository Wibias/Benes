package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/zcode"
)

const zcodeRestartMessage = "Restart ZCode to apply the change."

func runZcode(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  benes zcode enable [--origin URL]")
		fmt.Fprintln(stdout, "  benes zcode disable")
		fmt.Fprintln(stdout, "  benes zcode recover")
		fmt.Fprintln(stdout, "  benes zcode restore --generation N")
		return 0
	}
	switch args[0] {
	case "enable":
		return runZcodeEnable(ctx, args[1:], stdout, stderr, deps)
	case "disable":
		if err := zcode.Disable(ctx); err != nil {
			fmt.Fprintf(stderr, "benes zcode: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, zcodeRestartMessage)
		return 0
	case "recover":
		if err := zcode.Recover(ctx); err != nil {
			fmt.Fprintf(stderr, "benes zcode: %v\n", err)
			return 1
		}
		return 0
	case "restore":
		return runZcodeRestore(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "benes zcode: unknown command %q\n", args[0])
		return 2
	}
}

func runZcodeEnable(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	origin := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--origin" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes zcode enable: --origin requires a URL")
				return 2
			}
			origin = args[i+1]
			i++
			continue
		}
		fmt.Fprintf(stderr, "benes zcode enable: unknown flag %q\n", args[i])
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes zcode: %v\n", err)
		return 1
	}
	disk, err := deps.loadDiskConfig(paths.Config, 0)
	if err != nil {
		fmt.Fprintf(stderr, "benes zcode: %v\n", err)
		return 1
	}
	if origin == "" {
		origin, err = loopbackOrigin(disk)
		if err != nil {
			fmt.Fprintf(stderr, "benes zcode: %v\n", err)
			return 1
		}
	}
	if err := zcode.Enable(ctx, origin, bootstrap.ListCatalogModels(disk)); err != nil {
		fmt.Fprintf(stderr, "benes zcode: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, zcodeRestartMessage)
	return 0
}

func runZcodeRestore(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	genRaw := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--generation" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes zcode restore: --generation requires a number")
				return 2
			}
			genRaw = args[i+1]
			i++
			continue
		}
		fmt.Fprintf(stderr, "benes zcode restore: unknown flag %q\n", args[i])
		return 2
	}
	if genRaw == "" {
		fmt.Fprintln(stderr, "benes zcode restore: --generation is required")
		return 2
	}
	generation, err := strconv.ParseInt(genRaw, 10, 64)
	if err != nil || generation < 1 {
		fmt.Fprintln(stderr, "benes zcode restore: --generation must be a positive integer")
		return 2
	}
	if err := zcode.RestoreHistory(ctx, generation); err != nil {
		fmt.Fprintf(stderr, "benes zcode: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, zcodeRestartMessage)
	return 0
}

func loopbackOrigin(disk config.DiskConfig) (string, error) {
	listener, err := config.ProjectListener(disk)
	if err != nil {
		return "", err
	}
	host := listener.Hostname
	if ip := net.ParseIP(host); host == "" || host == "0.0.0.0" || host == "::" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(listener.Port)), nil
}
