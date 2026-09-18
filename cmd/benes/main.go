package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/Wibias/Benes/internal/codexshim"
)

var version = "dev"

func main() {
	if handled, code := codexshim.ForwardIfShim(os.Args[1:]); handled {
		os.Exit(code)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, args, stdout, stderr, defaultCommandDependencies())
}

func runWithDependencies(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "benes %s\n", version)
		return 0
	case "serve", "start":
		return runServe(ctx, args[1:], stderr, deps)
	case "stop":
		return runStop(stdout, stderr, deps)
	case "status":
		return runStatus(args[1:], stdout, stderr, deps)
	case "health":
		return runHealth(args[1:], stdout, stderr, deps)
	case "gui":
		return runGUI(args[1:], stdout, stderr, deps)
	case "dev":
		return runDev(ctx, args[1:], stdout, stderr, deps)
	case "ready":
		return runReady(args[1:], stdout, stderr, deps)

	case "ensure":
		return runEnsure(ctx, args[1:], stdout, stderr, deps)
	case "restart":
		return runRestart(ctx, args[1:], stdout, stderr, deps)
	case "restore", "eject":
		return runRestore(args[1:], stdout, stderr, deps)

	case "sync":
		return runSync(args[1:], stdout, stderr, deps)
	case "sync-cache":
		return runSyncCache(args[1:], stdout, stderr, deps)
	case "recover-history":
		return runRecoverHistory(args[1:], stdout, stderr)
	case "__refresh-version", "__gui-update-worker", "__tray-start", "__tray-restart", "__startup-health", "__tray-host":
		return runHidden(ctx, args[0], args[1:], stdout, stderr, deps)

	case "service":
		return runService(args[1:], stdout, stderr, deps)
	case "uninstall", "remove":
		return runUninstall(stdout, stderr, deps)
	case "codex-shim":
		return runCodexShim(args[1:], stdout, stderr, deps)

	case "config":
		return runConfig(args[1:], stdout, stderr, deps)
	case "init", "setup":
		return runInit(args[1:], os.Stdin, stdout, stderr, deps)
	case "models", "model":
		return runModels(args[1:], stdout, stderr, deps)
	case "provider":
		return runProvider(args[1:], stdout, stderr, deps)
	case "access":
		return runAccess(args[1:], stdout, stderr, deps)
	case "api-key":
		return runAPIKey(args[1:], stdout, stderr, deps)
	case "combo":
		return runCombo(args[1:], stdout, stderr, deps)
	case "alias":
		return runAlias(args[1:], stdout, stderr, deps)
	case "route":
		return runRoute(args[1:], stdout, stderr, deps)
	case "agent":
		return runAgent(args[1:], stdout, stderr, deps)
	case "v2":
		return runV2(args[1:], stdout, stderr, deps)

	case "doctor":
		return runDoctor(stdout, stderr, deps)
	case "debug":
		return runDebug(ctx, args[1:], stdout, stderr, deps)
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr, deps)
	case "logout":
		return runLogout(args[1:], stdout, stderr, deps)
	case "client":
		return runClient(ctx, args[1:], stdout, stderr)
	case "zcode":
		return runZcode(ctx, args[1:], stdout, stderr, deps)
	case "update":
		return runUpdate(args[1:], stdout, stderr)
	case "system":
		return runSystem(args[1:], stdout, stderr, deps)
	case "grok":
		return runGrok(args[1:], stdout, stderr, deps)
	case "claude":
		return runClaude(args[1:], stdout, stderr, deps)

	case "integration":
		return runIntegration(args[1:], stdout, stderr, deps)
	case "observe":
		return runObserve(ctx, args[1:], stdout, stderr, deps)
	case "logs":
		return runLogs(ctx, args[1:], stdout, stderr, deps)
	case "memory":
		return runMemory(args[1:], stdout, stderr, deps)
	case "storage":
		return runStorage(args[1:], stdout, stderr, deps)
	case "usage":
		return runUsage(args[1:], stdout, stderr, deps)
	case "account":
		return runAccount(args[1:], stdout, stderr, deps)
	case "export":
		return runExport(args[1:], stdout, stderr, deps)
	case "opencode":
		return runOpencode(args[1:], stdout, stderr, deps)
	case "mcode":
		return runMcode(args[1:], stdout, stderr, deps)
	case "mmx":
		return runMmx(args[1:], stdout, stderr, deps)
	case "tray":
		return runTray(args[1:], stdout, stderr, deps)
	case "lab":
		return runLab(args[1:], stdout, stderr, deps)

	default:
		if isFileClientShortcut(args[0]) {
			return runFileClientShortcut(args[0], args[1:], stdout, stderr, deps)
		}
		fmt.Fprintf(stderr, "benes: unknown command %q\n", args[0])
		printHelp(stderr)
		return 2
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Benes universal AI protocol gateway")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  benes <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  help       Show help")
	fmt.Fprintln(w, "  serve      Start the Go data plane [--port <port>] [--inject|--no-inject]")
	fmt.Fprintln(w, "  start      Alias of serve")
	fmt.Fprintln(w, "  stop       Stop the running data plane using the pid file")
	fmt.Fprintln(w, "  status     Show proxy runtime state and Codex routing")
	fmt.Fprintln(w, "  health     Exit 0 when runtime-port state exists; --json prints {ok,pid,port}")
	fmt.Fprintln(w, "  gui        Open the dashboard; start the data plane if it is not running")
	fmt.Fprintln(w, "  dev        Checkout GUI/data-plane for local work [--port 23200]")
	fmt.Fprintln(w, "  ready      Exit 0 only when /readyz reports ready; --wait [--timeout <seconds>]")

	fmt.Fprintln(w, "  ensure     Start the data plane if no runtime-port state exists")
	fmt.Fprintln(w, "  restart    Stop then start the data plane")
	fmt.Fprintln(w, "  restore    Restore native Codex config from the benes journal or strip managed routing")
	fmt.Fprintln(w, "  sync       Point Codex config at the running data plane")
	fmt.Fprintln(w, "  recover-history  Recover pre-backup OpenAI Codex App history (--legacy-openai)")

	fmt.Fprintln(w, "  sync-cache Refresh Codex models_cache.json from the on-disk catalog")

	fmt.Fprintln(w, "  service    status|start|stop|install|uninstall|repair")
	fmt.Fprintln(w, "  uninstall  Stop proxy, remove service/shim/tray, restore Codex")
	fmt.Fprintln(w, "  codex-shim status|install|uninstall|remove")
	fmt.Fprintln(w, "  config     Mutate durable config through a provenance-aware transaction")
	fmt.Fprintln(w, "  models     Catalog, custom models, probe, visibility, presets, new arrivals")
	fmt.Fprintln(w, "  provider   list|add|edit|remove|show|set-default|selected|account-mode")
	fmt.Fprintln(w, "  v2         status|on|off|mode <v1|default|v2>")

	fmt.Fprintln(w, "  combo      list|show|set|remove")
	fmt.Fprintln(w, "  alias      list|set|remove")
	fmt.Fprintln(w, "  route      combo <list|show|set|remove> | policy list|show")
	fmt.Fprintln(w, "  agent      status|injection|effort|subagents|fallback|sidecar")
	fmt.Fprintln(w, "  access     key list|create|remove | endpoints")
	fmt.Fprintln(w, "  doctor     Report local config, proxy, platform, and shim snapshot")
	fmt.Fprintln(w, "  lab        help|rebuild|status")

	fmt.Fprintln(w, "  login      Start or complete Cloud Code Assist or Kiro CLI login")
	fmt.Fprintln(w, "  logout     Delete a credential-store item by id")
	fmt.Fprintln(w, "  client     Apply managed-client files through the filesystem coordinator")
	fmt.Fprintln(w, "  export     Print a client config wired to the running proxy")
	fmt.Fprintln(w, "  opencode   Launch OpenCode wired to the running proxy")
	fmt.Fprintln(w, "  mcode      Launch MiniMax Code wired to the loopback proxy")
	printFileClientShortcutHelp(w)
	fmt.Fprintln(w, "  mmx        Launch MiniMax CLI text commands through a loopback bridge")
	fmt.Fprintln(w, "  tray       status|install|start|stop|uninstall (Windows)")

	fmt.Fprintln(w, "  zcode      Enable or disable the managed ZCode provider.benes fragment")
	fmt.Fprintln(w, "  update     Print the package-manager update policy; never mutates the install")
	fmt.Fprintln(w, "  version    Show version")
}
