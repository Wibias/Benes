package main

import (
	"fmt"
	"io"

	"github.com/Wibias/Benes/internal/codexshim"
)

func runCodexShim(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: codex-shim status|install|uninstall|remove")
		return 2
	}
	switch args[0] {
	case "status":
		homes := uninstallHomes(deps)
		if len(homes) == 0 {
			fmt.Fprintln(stdout, "Codex autostart shim is not installed.")
			return 0
		}
		for _, home := range homes {
			fmt.Fprintln(stdout, codexshim.Status(home))
		}
		return 0
	case "install":
		homes := uninstallHomes(deps)
		home := ""
		if len(homes) > 0 {
			home = homes[0]
		} else {
			fmt.Fprintln(stderr, "benes: could not resolve config home")
			return 1
		}
		ok, message, err := codexshim.Install(home)
		if err != nil {
			fmt.Fprintf(stderr, "benes: codex-shim install: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, message)
		if !ok {
			return 1
		}
		return 0
	case "uninstall", "remove":
		homes := uninstallHomes(deps)
		removed := false
		for _, home := range homes {
			ok, message, err := codexshim.Uninstall(home)
			if err != nil {
				fmt.Fprintf(stderr, "benes: codex-shim uninstall: %v\n", err)
				return 1
			}
			if ok {
				removed = true
				fmt.Fprintln(stdout, message)
			}
		}
		if !removed {
			fmt.Fprintln(stdout, "Codex autostart shim is not installed.")
		}
		return 0
	default:
		fmt.Fprintln(stderr, "benes: usage: codex-shim status|install|uninstall|remove")
		return 2
	}
}
