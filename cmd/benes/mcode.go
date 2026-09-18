package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const mcodeInstallHint = "`mcode` CLI not found. Install MiniMax Code first."

var launchMcode = defaultLaunchNamedClient

func runMcode(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	_ = stdout
	if err := ensureLiveProxy(stderr, deps); err != nil {
		return 1
	}
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintln(stderr, "benes: proxy not running. Start it with benes start.")
		return 1
	}
	if !isLoopbackHost(probeHost(hostFromBase(base))) {
		fmt.Fprintln(stderr, "benes: MiniMax Code integration is loopback-only; its config cannot carry a remote-admission header.")
		return 2
	}
	if code := runClientIntegration([]string{"enable", "--client", "mcode"}, stdout, stderr, deps); code != 0 {
		return code
	}
	text, err := os.ReadFile(mcodeConfigPath())
	if err != nil {
		fmt.Fprintln(stderr, "benes: MiniMax Code is not connected. Re-run: benes mcode")
		return 2
	}
	configured := mcodeBenesBaseURL(string(text))
	if configured == "" {
		fmt.Fprintln(stderr, "benes: MiniMax Code is not connected. Re-run: benes mcode")
		return 2
	}
	u, _ := url.Parse(base)
	expected := fmt.Sprintf("http://%s:%s", probeHost(u.Hostname()), u.Port())
	if normalizeOrigin(configured) != normalizeOrigin(expected) {
		fmt.Fprintln(stderr, "benes: MiniMax Code's Benes provider points at a stale proxy address. Re-run: benes mcode")
		return 2
	}
	fmt.Fprintf(stderr, "MiniMax Code wired to %s; select custom_provider:benes/<model> in MCode.\n", expected)

	if err := launchMcode("mcode", args, os.Environ()); err != nil {
		if isOpencodeMissing(err) {
			fmt.Fprintln(stderr, mcodeInstallHint)
			return 1
		}
		fmt.Fprintf(stderr, "benes: failed to launch mcode: %v\n", err)
		return 1
	}
	return 0
}

func mcodeConfigPath() string {
	if dir := strings.TrimSpace(os.Getenv("MINIMAX_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".minimax", "config.yaml")
}

func mcodeBenesBaseURL(text string) string {
	inCustom, inProvider, inOptions := false, false, false
	customIndent, providerIndent, optionsIndent := -1, -1, -1
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		key, value, ok := yamlKeyValue(strings.TrimSpace(trimmed))
		if !ok {
			continue
		}
		if inOptions && indent <= optionsIndent {
			inOptions = false
		}
		if inProvider && indent <= providerIndent {
			inProvider, inOptions = false, false
		}
		if inCustom && indent <= customIndent {
			inCustom, inProvider, inOptions = false, false, false
		}
		switch {
		case !inCustom && key == "custom_provider":
			inCustom = true
			customIndent = indent
		case inCustom && !inProvider && key == "benes":
			inProvider = true
			providerIndent = indent
		case inProvider && !inOptions && key == "options":
			inOptions = true
			optionsIndent = indent
		case inOptions && key == "baseURL":
			return strings.Trim(value, `"'`)
		}
	}
	return ""
}

func yamlKeyValue(line string) (string, string, bool) {
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

func hostFromBase(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func probeHost(host string) string {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	default:
		return host
	}
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/")
}

func defaultLaunchNamedClient(name string, args []string, env []string) error {
	bin, err := exec.LookPath(name)
	if err != nil && runtime.GOOS == "windows" {
		bin, err = exec.LookPath(name + ".cmd")
	}
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}
