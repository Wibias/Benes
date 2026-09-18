package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providers/kiro"
	"github.com/Wibias/Benes/internal/runtimestate"
)

type commandDependencies struct {
	resolvePaths            func(config.PathOptions) (config.Paths, error)
	loadDiskConfig          func(string, int64) (config.DiskConfig, error)
	resolveEnvironmentToken func(map[string]string) string
	buildDataPlane          func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error)
	serveDataPlane          func(context.Context, bootstrap.DataPlane, bootstrap.ServeOptions) error
	kiroHost                func() kiro.Host
	kiroRunner              kiro.CLIRunner
	spawnStart              func() error
	openURL                 func(string) error
	sleep                   func(time.Duration)
	now                     func() time.Time
	probeReady              func(host string, port int) readyProbe
	codexAppServerIO        func() codexappserver.IO
	inspectProcess          func(pid int) runtimestate.ProcessInfo
	probeListener           func(host string, port int) bool
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		resolvePaths:            config.ResolvePaths,
		loadDiskConfig:          config.LoadDiskConfig,
		resolveEnvironmentToken: config.ResolveEnvironmentDataPlaneToken,
		buildDataPlane:          bootstrap.BuildDataPlane,
		serveDataPlane:          bootstrap.ServeDataPlane,
	}
}

func runServe(ctx context.Context, args []string, stderr io.Writer, deps commandDependencies) int {
	flags, err := parseServeFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "benes: serve: %v\n", err)
		return 2
	}
	if ctx == nil {
		fmt.Fprintln(stderr, "benes: serve: context is required")
		return 1
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "benes: serve: %v\n", err)
		return 1
	}

	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	overrides := loadServeRuntimeOverrides()
	disk, err := deps.loadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}

	var runtimeTokens []string
	if token := deps.resolveEnvironmentToken(nil); token != "" {
		runtimeTokens = []string{token}
	}
	plane, err := deps.buildDataPlane(ctx, disk, bootstrap.DataPlaneOptions{
		RuntimeDataPlaneTokens: runtimeTokens,
		ConfigPath:             paths.Config,
		CodexPool: bootstrap.CodexPoolOptions{
			BenesHome: paths.Home,
		},
	})
	if err != nil {
		return serveFailure(stderr, "build data plane", err)
	}
	if err := ctx.Err(); err != nil {
		return serveFailure(stderr, "startup cancelled", err)
	}
	if flags.port != 0 {
		plane.Listener.Port = flags.port
	}
	pidPath := paths.PID
	runtimePortPath := paths.RuntimePort
	if overrides.PIDPath != "" {
		pidPath = overrides.PIDPath
	}
	if overrides.RuntimePortPath != "" {
		runtimePortPath = overrides.RuntimePortPath
	}
	var afterListen func(string, int) error
	if flags.inject && !overrides.SkipCodexInject {
		afterListen = func(hostname string, listenPort int) error {
			home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
			if err != nil {
				return nil
			}
			host := hostname
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			if _, err = codexrestore.Inject(home, fmt.Sprintf("http://%s:%d/v1", host, listenPort)); err != nil {
				return err
			}
			codexappserver.WarnIfStaleAfterStartupWrite(stdLogger{out: stderr, err: stderr}, appserverIO(deps, home))
			return nil
		}
	}
	if err := deps.serveDataPlane(ctx, plane, bootstrap.ServeOptions{
		PIDPath:         pidPath,
		RuntimePortPath: runtimePortPath,
		AfterListen:     afterListen,
	}); err != nil {
		return serveFailure(stderr, "serve data plane", err)
	}

	return 0
}

func serveFailure(stderr io.Writer, stage string, err error) int {
	fmt.Fprintf(stderr, "benes: serve: %s: %v\n", stage, err)
	return 1
}

type serveRuntimeOverrides struct {
	PIDPath         string
	RuntimePortPath string
	SkipCodexInject bool
}

func loadServeRuntimeOverrides() serveRuntimeOverrides {
	return loadServeRuntimeOverridesFrom(os.Getenv)
}

func loadServeRuntimeOverridesFrom(getenv func(string) string) serveRuntimeOverrides {
	if getenv == nil {
		return serveRuntimeOverrides{}
	}
	skipInject := strings.TrimSpace(getenv("BENES_SKIP_CODEX_INJECT")) == "1"
	if strings.TrimSpace(getenv("BENES_DEV_MODE")) != "1" {
		return serveRuntimeOverrides{SkipCodexInject: skipInject}
	}
	return serveRuntimeOverrides{
		PIDPath:         strings.TrimSpace(getenv("BENES_PID_PATH")),
		RuntimePortPath: strings.TrimSpace(getenv("BENES_RUNTIME_PORT_PATH")),
		SkipCodexInject: skipInject,
	}
}

type serveFlags struct {
	port   int
	inject bool
}

func parseServeFlags(args []string) (serveFlags, error) {
	var flags serveFlags
	sawInject := false
	sawNoInject := false
	usage := "Usage: benes serve [--port <port>] [--inject|--no-inject]"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 >= len(args) {
				return serveFlags{}, fmt.Errorf("%s", usage)
			}
			port, err := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if err != nil || port < 1 || port > 65535 {
				return serveFlags{}, fmt.Errorf("port must be from 1 through 65535")
			}
			flags.port = port
			i++
		case "--inject":
			sawInject = true
		case "--no-inject":
			sawNoInject = true
		default:
			return serveFlags{}, fmt.Errorf("does not accept arguments")
		}
	}
	if sawInject && sawNoInject {
		return serveFlags{}, fmt.Errorf("cannot combine --inject and --no-inject")
	}
	flags.inject = sawInject
	return flags, nil
}
