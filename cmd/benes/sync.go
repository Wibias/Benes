package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/codexcache"
	"github.com/Wibias/Benes/internal/codexcatalog"
	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/router"
)

func runSync(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	restartCodex := false
	for _, arg := range args {
		if arg == "--restart-codex" {
			restartCodex = true
			continue
		}
		fmt.Fprintln(stderr, "benes: usage: sync [--restart-codex]")
		return 2
	}

	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	if err != nil || state.PID <= 0 || state.Port <= 0 {
		fmt.Fprintln(stderr, "benes: no running proxy found. Start it with: benes start")
		return 1
	}
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve CODEX_HOME: %v\n", err)
		return 1
	}
	host := strings.TrimSpace(state.Hostname)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	result, err := codexrestore.Inject(home, fmt.Sprintf("http://%s:%d/v1", host, state.Port))
	if err != nil {
		fmt.Fprintf(stderr, "benes: sync: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, result.Message)
	models, aliases := catalogModelsAndAliasesFromPaths(deps, paths)
	catalogChanged, err := refreshCodexPickerCatalogWithAliases(home, models, aliases)
	if err != nil {
		fmt.Fprintf(stderr, "benes: sync: %v\n", err)
		return 1
	}
	if result.Changed || catalogChanged {
		afterSyncWrite(stdout, stderr, restartCodex, appserverIO(deps, home))
	}
	return 0
}

func catalogModelsAndAliasesFromPaths(deps commandDependencies, paths config.Paths) ([]catalog.Model, router.AliasTable) {
	if strings.TrimSpace(paths.Config) == "" {
		return nil, router.AliasTable{}
	}
	load := deps.loadDiskConfig
	if load == nil {
		load = config.LoadDiskConfig
	}
	disk, err := load(paths.Config, 0)
	if err != nil {
		return nil, router.AliasTable{}
	}
	return bootstrap.ListCatalogModels(disk), config.ProjectRouteAliases(disk)
}

func refreshCodexPickerCatalog(home string, models []catalog.Model) (bool, error) {
	return refreshCodexPickerCatalogWithAliases(home, models, router.AliasTable{})
}

func refreshCodexPickerCatalogWithAliases(home string, models []catalog.Model, aliases router.AliasTable) (bool, error) {
	wrote, err := codexcatalog.WriteWithAliases(home, models, aliases)
	if err != nil {
		return false, err
	}
	cacheWrote, err := codexcache.Invalidate(home)
	if err != nil {
		return false, err
	}
	return wrote || cacheWrote, nil
}

type stdLogger struct {
	out io.Writer
	err io.Writer
}

func (l stdLogger) Log(msg string) {
	if l.out == nil || msg == "" {
		return
	}
	fmt.Fprintln(l.out, msg)
}

func (l stdLogger) Error(msg string) {
	if l.err == nil || msg == "" {
		return
	}
	fmt.Fprintln(l.err, msg)
}

func appserverIO(deps commandDependencies, home string) codexappserver.IO {
	if deps.codexAppServerIO != nil {
		io := deps.codexAppServerIO()
		if strings.TrimSpace(io.CatalogHome) == "" {
			io.CatalogHome = home
		}
		return io
	}
	return codexappserver.IO{CatalogHome: home}
}

func afterSyncWrite(stdout, stderr io.Writer, restart bool, ioOptions codexappserver.IO) {
	codexappserver.AfterCatalogWrite(codexappserver.AfterWriteOptions{
		Restart: restart,
		Log:     stdLogger{out: stdout, err: stderr},
		IO:      ioOptions,
	})
}
