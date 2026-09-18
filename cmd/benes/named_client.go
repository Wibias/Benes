package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Wibias/Benes/internal/export"
)

// opencode and mcode already have custom launchers. Everything else in
// export.ClientIDs gets `benes <id>`: enable the integration, then exec the CLI.
var dedicatedFileClientCommands = map[string]struct{}{
	"opencode": {},
	"mcode":    {},
}

var launchNamedClient = defaultLaunchNamedClient

func fileClientShortcutIDs() []string {
	out := make([]string, 0, len(export.ClientIDs))
	for _, id := range export.ClientIDs {
		if _, skip := dedicatedFileClientCommands[id]; skip {
			continue
		}
		out = append(out, id)
	}
	return out
}

func isFileClientShortcut(id string) bool {
	for _, item := range fileClientShortcutIDs() {
		if item == id {
			return true
		}
	}
	return false
}

func fileClientLoopbackOnly(id string) bool {
	for _, item := range export.Clients() {
		if item.ID == id {
			return item.LoopbackOnly
		}
	}
	return false
}

func runFileClientShortcut(client string, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if err := ensureLiveProxy(stderr, deps); err != nil {
		return 1
	}
	if fileClientLoopbackOnly(client) {
		base, err := liveProxyBase(deps)
		if err != nil {
			fmt.Fprintln(stderr, "benes: proxy not running. Start it with benes start.")
			return 1
		}
		if !isLoopbackHost(probeHost(hostFromBase(base))) {
			fmt.Fprintf(stderr, "benes: %s integration is loopback-only; its config cannot carry a remote-admission header.\n", client)
			return 2
		}
	}
	if code := runClientIntegration([]string{"enable", "--client", client}, stdout, stderr, deps); code != 0 {
		return code
	}
	if err := launchNamedClient(client, args, os.Environ()); err != nil {
		if isOpencodeMissing(err) {
			fmt.Fprintf(stderr, "`%s` CLI not found. Install it first.\n", client)
			return 1
		}
		fmt.Fprintf(stderr, "benes: failed to launch %s: %v\n", client, err)
		return 1
	}
	return 0
}

func printFileClientShortcutHelp(w io.Writer) {
	ids := fileClientShortcutIDs()
	if len(ids) == 0 {
		return
	}
	fmt.Fprintf(w, "  %-10s Enable and launch that client against the running proxy\n", strings.Join(ids, ", "))
}
