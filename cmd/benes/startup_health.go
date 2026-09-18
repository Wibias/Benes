package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/codexrouting"
	"github.com/Wibias/Benes/internal/codexshim"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/startuphealth"
)

func collectStartupHealth(deps commandDependencies) startuphealth.Report {
	kind := codexrouting.KindNative
	if home, err := config.ResolveCodexHome(config.CodexHomeOptions{}); err == nil {
		if raw, err := os.ReadFile(filepath.Join(home, "config.toml")); err == nil {
			kind = codexrouting.Classify(string(raw))
		} else if !os.IsNotExist(err) {
			kind = codexrouting.KindUnknown
		}
	} else {
		kind = codexrouting.KindUnknown
	}
	autostart := true
	if root, err := loadConfigRawObject(deps); err == nil {
		var enabled bool
		if json.Unmarshal(root["codexAutoStart"], &enabled) == nil {
			autostart = enabled
		}
	}
	shimInstalled := false
	shimHealthy := false
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err == nil {
		status := codexshim.Status(paths.Home)
		if strings.Contains(status, "installed") && !strings.Contains(status, "not installed") {
			shimInstalled = true
			shimHealthy = true
		}
	}
	supported := runtime.GOOS == "windows" || runtime.GOOS == "linux"
	task := probeSchedulerTask(schedulerTaskName)
	installed := task.Status == schedulerTaskPresent
	return startuphealth.Derive(startuphealth.Inputs{
		RoutingKind:      kind,
		AutostartEnabled: autostart,
		ServiceInstalled: installed,
		ServiceViable:    installed,
		ServiceEnabled:   installed,
		ServiceRunning:   installed,
		ServiceSupported: supported,
		ShimInstalled:    shimInstalled,
		ShimHealthy:      shimHealthy,
		Platform:         runtime.GOOS,
	})
}

func writeStartupHealth(stdout io.Writer, deps commandDependencies) int {
	enc := json.NewEncoder(stdout)
	_ = enc.Encode(collectStartupHealth(deps))
	return 0
}
