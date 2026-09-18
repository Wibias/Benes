package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

const (
	devUsage = "benes: usage: dev [--port <n>] [--dir <path>] [--update] [--branch <name>] [--proxy on|off] [--open] [--no-gui]"
)

type devOptions struct {
	Port    int
	Dir     string
	Update  bool
	ProxyOn bool
	Branch  string
	Open    bool
	NoGUI   bool
}

func runDev(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	opts, err := parseDevArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "benes: dev: %v\n", err)
		fmt.Fprintln(stderr, devUsage)
		return 2
	}
	if ctx == nil {
		fmt.Fprintln(stderr, "benes: dev: context is required")
		return 1
	}
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "benes: dev: resolve --dir: %v\n", err)
		return 1
	}
	if err := validateDevCheckout(dir); err != nil {
		fmt.Fprintf(stderr, "benes: dev: %v\n", err)
		return 1
	}
	if opts.Update {
		if err := pullDevBranch(ctx, dir, opts.Branch, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "benes: dev: --update: %v\n", err)
			return 1
		}
	}

	mainState, mainErr := loadMainRuntimeState(deps)
	if !opts.ProxyOn {
		if mainErr != nil || mainState.Port <= 0 {
			fmt.Fprintln(stderr, "benes: dev: --proxy off needs a running main listener (benes start)")
			return 1
		}
	} else if mainErr != nil || mainState.Port <= 0 {
		fmt.Fprintln(stdout, "note: main listener is not running; using ~/.benes provider config and auth as-is")
	}

	stateDir := ""
	if opts.ProxyOn {
		stateDir = filepath.Join(dir, ".benes-dev")
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			fmt.Fprintf(stderr, "benes: dev: create %s: %v\n", stateDir, err)
			return 1
		}
	}

	guiPort := opts.Port
	goPort := opts.Port
	if opts.ProxyOn && !opts.NoGUI {
		free, err := pickFreeLoopbackPort()
		if err != nil {
			fmt.Fprintf(stderr, "benes: dev: pick data-plane port: %v\n", err)
			return 1
		}
		goPort = free
	}

	var proxyTarget string
	if opts.ProxyOn {
		proxyTarget = fmt.Sprintf("http://127.0.0.1:%d", goPort)
	} else {
		host := strings.TrimSpace(mainState.Hostname)
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		proxyTarget = fmt.Sprintf("http://%s:%d", host, mainState.Port)
		if err := pingHTTP(ctx, proxyTarget+"/healthz"); err != nil {
			fmt.Fprintln(stderr, "benes: dev: --proxy off needs a running main listener (benes start)")
			return 1
		}
	}

	fmt.Fprintf(stdout, "benes dev  port=%d  dir=%s  proxy=%s  update=%v  branch=%s\n",
		opts.Port, dir, onOffLower(opts.ProxyOn), opts.Update, opts.Branch)
	if opts.ProxyOn {
		fmt.Fprintf(stdout, "  data-plane %s (providers from ~/.benes, no Codex inject)\n", proxyTarget)
		if !opts.NoGUI {
			fmt.Fprintf(stdout, "  gui        http://127.0.0.1:%d  (Vite HMR → data-plane)\n", guiPort)
		}
	} else {
		fmt.Fprintf(stdout, "  gui        http://127.0.0.1:%d  (Vite HMR → main %s)\n", guiPort, proxyTarget)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errc := make(chan error, 2)
	if opts.ProxyOn {
		go func() {
			errc <- superviseDevDataPlane(runCtx, dir, stateDir, goPort, stdout, stderr)
		}()
		if waitErr := waitHTTP(runCtx, proxyTarget+"/healthz", 45*time.Second); waitErr != nil {
			cancel()
			fmt.Fprintf(stderr, "benes: dev: data plane did not become ready: %v\n", waitErr)
			return 1
		}
	}
	if !opts.NoGUI {
		go func() {
			errc <- runViteDev(runCtx, dir, guiPort, proxyTarget, stdout, stderr)
		}()
	}

	openURL := "http://127.0.0.1:" + strconv.Itoa(opts.Port)
	if opts.Open {
		open := deps.openURL
		if open == nil {
			open = defaultOpenURL
		}
		if err := open(openURL); err != nil {
			fmt.Fprintf(stderr, "benes: dev: open %s: %v\n", openURL, err)
		}
	}

	select {
	case <-ctx.Done():
		return 0
	case err := <-errc:
		cancel()
		if err == nil || runCtx.Err() != nil {
			return 0
		}
		fmt.Fprintf(stderr, "benes: dev: %v\n", err)
		return 1
	}
}

func parseDevArgs(args []string) (devOptions, error) {
	opts := devOptions{Dir: ".", Branch: "dev", Port: config.DefaultDevPort}
	for i := 0; i < len(args); i++ {
		name, inline, hasInline := splitDevFlag(args[i])
		need := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--port":
			raw, err := need()
			if err != nil {
				return devOptions{}, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || n < 1 || n > 65535 {
				return devOptions{}, fmt.Errorf("--port must be from 1 through 65535")
			}
			opts.Port = n
		case "--dir":
			raw, err := need()
			if err != nil {
				return devOptions{}, err
			}
			if strings.TrimSpace(raw) == "" {
				return devOptions{}, fmt.Errorf("--dir requires a path")
			}
			opts.Dir = raw
		case "--branch":
			raw, err := need()
			if err != nil {
				return devOptions{}, err
			}
			if strings.TrimSpace(raw) == "" {
				return devOptions{}, fmt.Errorf("--branch requires a name")
			}
			opts.Branch = strings.TrimSpace(raw)
		case "--proxy":
			raw, err := need()
			if err != nil {
				return devOptions{}, err
			}
			on, ok := parseOnOff(raw)
			if !ok {
				return devOptions{}, fmt.Errorf("--proxy must be on or off")
			}
			opts.ProxyOn = on
		case "--update":
			if hasInline {
				return devOptions{}, fmt.Errorf("--update does not take a value")
			}
			opts.Update = true
		case "--open":
			if hasInline {
				return devOptions{}, fmt.Errorf("--open does not take a value")
			}
			opts.Open = true
		case "--no-gui":
			if hasInline {
				return devOptions{}, fmt.Errorf("--no-gui does not take a value")
			}
			opts.NoGUI = true
		default:
			return devOptions{}, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	if opts.NoGUI && !opts.ProxyOn {
		return devOptions{}, fmt.Errorf("--no-gui requires --proxy on")
	}
	return opts, nil
}

func splitDevFlag(arg string) (name, value string, hasValue bool) {
	if strings.HasPrefix(arg, "--") {
		if i := strings.IndexByte(arg, '='); i >= 0 {
			return arg[:i], arg[i+1:], true
		}
	}
	return arg, "", false
}

func validateDevCheckout(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return fmt.Errorf("%s is not a Benes checkout (missing go.mod)", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "benes")); err != nil {
		return fmt.Errorf("%s is not a Benes checkout (missing cmd/benes)", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "gui", "package.json")); err != nil {
		return fmt.Errorf("%s is not a Benes checkout (missing gui/package.json)", dir)
	}
	body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	if !strings.Contains(string(body), "module github.com/Wibias/Benes") {
		return fmt.Errorf("%s go.mod is not the Benes module", dir)
	}
	return nil
}

func loadMainRuntimeState(deps commandDependencies) (runtimePortState, error) {
	resolve := deps.resolvePaths
	if resolve == nil {
		resolve = config.ResolvePaths
	}
	paths, err := resolve(config.PathOptions{})
	if err != nil {
		return runtimePortState{}, err
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	if err != nil || state.PID <= 0 || state.Port <= 0 {
		return runtimePortState{}, fmt.Errorf("main listener is not running")
	}
	return state, nil
}

func pullDevBranch(ctx context.Context, dir, branch string, stdout, stderr io.Writer) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git is not on PATH")
	}
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Dir = dir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	}
	if err := run("rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("%s is not a git checkout", dir)
	}
	if err := run("fetch", "origin", branch); err != nil {
		return fmt.Errorf("git fetch origin %s: %w", branch, err)
	}
	if err := run("pull", "--ff-only", "origin", branch); err != nil {
		return fmt.Errorf("git pull --ff-only origin %s: %w", branch, err)
	}
	return nil
}

func pickFreeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("no tcp port")
	}
	return addr.Port, nil
}

func pingHTTP(ctx context.Context, rawURL string) error {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%s: status %d", rawURL, resp.StatusCode)
	}
	return nil
}

func waitHTTP(ctx context.Context, rawURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", rawURL)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func runViteDev(ctx context.Context, dir string, port int, proxyTarget string, stdout, stderr io.Writer) error {
	guiDir := filepath.Join(dir, "gui")
	if _, err := os.Stat(filepath.Join(guiDir, "node_modules")); err != nil {
		fmt.Fprintln(stdout, "benes dev: installing gui dependencies (npm ci)")
		ci := exec.CommandContext(ctx, npmName(), "ci")
		ci.Dir = guiDir
		ci.Stdout = stdout
		ci.Stderr = stderr
		if err := ci.Run(); err != nil {
			return fmt.Errorf("npm ci in gui/: %w", err)
		}
	}
	cmd := exec.CommandContext(ctx, npmName(), "run", "dev", "--", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--strictPort")
	cmd.Dir = guiDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = overlayEnv(os.Environ(), []string{"BENES_PROXY_TARGET=" + proxyTarget})
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start vite: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		killCmd(cmd)
		<-wait
		return ctx.Err()
	case err := <-wait:
		return err
	}
}

func superviseDevDataPlane(ctx context.Context, dir, stateDir string, port int, stdout, stderr io.Writer) error {
	var current *exec.Cmd
	restart := make(chan struct{}, 1)
	kick := func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	}
	go watchGoSources(ctx, dir, kick)
	kick()
	for {
		select {
		case <-ctx.Done():
			killCmd(current)
			return ctx.Err()
		case <-restart:
			killCmd(current)
			current = nil
			bin, err := buildDevBinary(ctx, dir, stateDir, stdout, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "benes dev: rebuild failed: %v\n", err)
				continue
			}
			cmd, err := startDevBinary(dir, stateDir, bin, port, stdout, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "benes dev: start data plane: %v\n", err)
				continue
			}
			current = cmd
			go func(running *exec.Cmd) {
				if err := running.Wait(); err != nil && ctx.Err() == nil {
					fmt.Fprintf(stderr, "benes dev: data plane exited: %v\n", err)
				}
			}(cmd)
		}
	}
}

func buildDevBinary(ctx context.Context, dir, stateDir string, stdout, stderr io.Writer) (string, error) {
	name := "benes-dev"
	if runtime.GOOS == "windows" {
		name = "benes-dev.exe"
	}
	out := filepath.Join(stateDir, name)
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/benes")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out, nil
}

func startDevBinary(dir, stateDir, bin string, port int, stdout, stderr io.Writer) (*exec.Cmd, error) {
	cmd := exec.Command(bin, "start", "--port", strconv.Itoa(port))
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = overlayEnv(os.Environ(), []string{
		"BENES_DEV_MODE=1",
		"BENES_SKIP_CODEX_INJECT=1",
		"BENES_PID_PATH=" + filepath.Join(stateDir, "benes.pid"),
		"BENES_RUNTIME_PORT_PATH=" + filepath.Join(stateDir, "runtime-port.json"),
	})
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func watchGoSources(ctx context.Context, dir string, kick func()) {
	stamp := goSourceStamp(dir)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next := goSourceStamp(dir)
			if next != stamp && next != 0 {
				stamp = next
				kick()
			}
		}
	}
}

func goSourceStamp(dir string) int64 {
	var latest int64
	roots := []string{
		filepath.Join(dir, "cmd"),
		filepath.Join(dir, "internal"),
		filepath.Join(dir, "go.mod"),
		filepath.Join(dir, "go.sum"),
	}
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				name := info.Name()
				if name == "node_modules" || name == "dist" || name == ".git" || name == ".benes-dev" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "go.mod") || strings.HasSuffix(path, "go.sum") {
				if info.ModTime().UnixNano() > latest {
					latest = info.ModTime().UnixNano()
				}
			}
			return nil
		})
	}
	return latest
}

func killCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return
	}
	_ = cmd.Process.Kill()
}

func overlayEnv(base []string, extra []string) []string {
	keys := make(map[string]struct{}, len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		keys[envKey(name)] = struct{}{}
		out = append(out, kv)
	}
	for _, kv := range base {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, skip := keys[envKey(name)]; skip {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func envKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func npmName() string {
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("npm.cmd"); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("npm"); err == nil {
		return path
	}
	return "npm"
}
