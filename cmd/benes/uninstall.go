package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Wibias/Benes/internal/codexshim"
	"github.com/Wibias/Benes/internal/config"
)

func runUninstall(stdout, stderr io.Writer, deps commandDependencies) int {
	failed := 0
	step := func(label string, fn func() (bool, string, error)) {
		changed, message, err := fn()
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "benes: %s failed: %v\n", label, err)
			return
		}
		if !changed {
			if message == "" {
				fmt.Fprintf(stdout, "- %s: not installed\n", label)
				return
			}
			fmt.Fprintf(stdout, "- %s: %s\n", label, message)
			return
		}
		if message == "" {
			fmt.Fprintf(stdout, "%s\n", label)
			return
		}
		fmt.Fprintf(stdout, "%s\n", message)
	}

	step("service removed", func() (bool, string, error) {
		code := runServiceUninstall(stdout, stderr, deps)
		if code != 0 {
			return false, "", fmt.Errorf("exit %d", code)
		}
		return true, "service removed", nil
	})
	step("proxy stopped", func() (bool, string, error) {
		code := runStop(stdout, stderr, deps)
		if code != 0 {
			return false, "", fmt.Errorf("exit %d", code)
		}
		return true, "proxy stopped", nil
	})
	step("Codex autostart shim removed", func() (bool, string, error) {
		homes := uninstallHomes(deps)
		var last string
		removed := false
		for _, home := range homes {
			ok, message, err := codexshim.Uninstall(home)
			if err != nil {
				return false, "", err
			}
			if ok {
				removed = true
				last = message
			} else if last == "" {
				last = message
			}
		}
		return removed, last, nil
	})
	if runtime.GOOS == "windows" {
		step("Windows tray removed", func() (bool, string, error) {
			return uninstallWindowsTray(deps)
		})
	}

	if failed > 0 {
		fmt.Fprintf(stderr, "benes: uninstall finished with %d failed step(s)\n", failed)
		return 1
	}
	fmt.Fprintln(stdout, "local lifecycle state removed.")
	return 0
}

func uninstallHomes(deps commandDependencies) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(home string) {
		home = filepath.Clean(home)
		if home == "" || home == "." {
			return
		}
		if _, ok := seen[home]; ok {
			return
		}
		seen[home] = struct{}{}
		out = append(out, home)
	}
	if deps.resolvePaths != nil {
		if paths, err := deps.resolvePaths(config.PathOptions{}); err == nil {
			add(paths.Home)
		}
	} else if paths, err := config.ResolvePaths(config.PathOptions{}); err == nil {
		add(paths.Home)
	}
	if user, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(user, ".benes"))
		add(filepath.Join(user, ".benes"))
	}
	return out
}
