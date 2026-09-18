package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/codexshim"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/platform"
	"github.com/Wibias/Benes/internal/transport"
)

func runDoctor(stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	policy := transport.PolicyFromConfig(disk.Proxy, disk.NoProxy)
	probe := platform.NewProbe()
	snap := probe.Snapshot()
	fmt.Fprintf(stdout, "config\t%s\n", paths.Config)
	fmt.Fprintf(stdout, "home\t%s\n", paths.Home)
	if codexHome, err := config.ResolveCodexHome(config.CodexHomeOptions{}); err == nil {
		fmt.Fprintf(stdout, "codex_home\t%s\n", codexHome)
	} else {
		fmt.Fprintf(stdout, "codex_home\tunresolved\t%v\n", err)
	}
	fmt.Fprintf(stdout, "proxy_mode\t%s\n", policy.Mode)
	fmt.Fprintf(stdout, "platform\t%s\n", snap.ServiceManager)
	fmt.Fprintf(stdout, "wsl\t%t\n", doctorIsWSL())
	fmt.Fprintf(stdout, "providers\t%d\n", len(disk.Providers))
	if state, err := readRuntimePortState(paths.RuntimePort); evaluateRuntime(state, err, deps).Running() {
		fmt.Fprintf(stdout, "proxy\trunning\t%d\t%d\n", state.PID, state.Port)
	} else {
		fmt.Fprintln(stdout, "proxy\tnot-running")
	}
	for _, home := range uninstallHomes(deps) {
		fmt.Fprintf(stdout, "shim\t%s\t%s\n", home, codexshim.Status(home))
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if _, ok := os.LookupEnv(key); ok {
			fmt.Fprintf(stdout, "env\t%s\tpresent\n", key)
		} else {
			fmt.Fprintf(stdout, "env\t%s\tabsent\n", key)
		}
	}
	writeDoctorCredentials(stdout, paths.Home, disk)
	return 0
}

func doctorIsWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	body, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "microsoft")
}

func writeDoctorCredentials(stdout io.Writer, home string, disk config.DiskConfig) {
	var store credentials.Store
	if dir := strings.TrimSpace(home); dir != "" {
		if s, err := credentials.NewFileStore(filepath.Join(dir, "credentials"), true); err == nil {
			store = s
		}
	}
	for id, raw := range disk.Providers {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		if refRaw, ok := obj["credentialRef"]; ok {
			var ref credentials.Ref
			if json.Unmarshal(refRaw, &ref) != nil {
				continue
			}
			st := credentials.StatusOf(store, ref)
			fmt.Fprintf(stdout, "credential\t%s\t%s\t%t\n", id, st.Source, st.Available)
			continue
		}
		if keyRaw, ok := obj["apiKey"]; ok {
			var key string
			if json.Unmarshal(keyRaw, &key) == nil && strings.TrimSpace(key) != "" {
				fmt.Fprintf(stdout, "credential\t%s\tplaintext\ttrue\n", id)
			}
		}
	}
}
