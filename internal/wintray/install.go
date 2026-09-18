package wintray

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

type State struct {
	Version      int    `json:"version"`
	CLI          string `json:"cli"`
	Script       string `json:"script"`
	CodexHome    string `json:"codexHome"`
	BenesHome    string `json:"benesHome"`
	LauncherPath string `json:"launcherPath"`
	RunValue     string `json:"runValue"`
	RunCommand   string `json:"runCommand"`
}

type Status struct {
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
	Summary   string `json:"summary"`
	RunValue  string `json:"runValue,omitempty"`
}

var addRunValueFn = addRunValue

func Install(home, cli, codexHome string) (Status, error) {
	if runtime.GOOS != "windows" {
		return Status{Supported: false, Summary: "unsupported on " + runtime.GOOS}, nil
	}
	if err := persist(home, cli, codexHome); err != nil {
		return Status{}, err
	}
	runValue := RunValue(home)
	runCommand, err := RunCommand(LauncherPath(home))
	if err != nil {
		return Status{}, err
	}
	if err := addRunValueFn(runValue, runCommand); err != nil {
		return Status{}, err
	}
	return QueryStatus(home), nil
}

func persist(home, cli, codexHome string) error {
	if strings.TrimSpace(home) == "" || strings.TrimSpace(cli) == "" {
		return fmt.Errorf("tray install requires home and CLI paths")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	entry := Entry{
		CLI:       cli,
		Script:    ScriptPath(home),
		CodexHome: codexHome,
		BenesHome: home,
	}
	script, err := EmbeddedScript()
	if err != nil {
		return err
	}
	if !strings.Contains(string(script), "$psi.FileName = $CliPath") {
		return fmt.Errorf("embedded tray script must exec the CLI path")
	}
	if err := atomicfile.Write(entry.Script, script, atomicfile.Options{Mode: 0o600}); err != nil {
		return err
	}
	icons, err := EmbeddedIcons()
	if err != nil {
		return err
	}
	for _, name := range []string{
		"benes-tray-online.ico",
		"benes-tray-warning.ico",
		"benes-tray-offline.ico",
	} {
		body, ok := icons[name]
		if !ok {
			return fmt.Errorf("missing embedded tray icon %s", name)
		}
		if err := atomicfile.Write(filepath.Join(home, name), body, atomicfile.Options{Mode: 0o600}); err != nil {
			return err
		}
	}
	launcher := LauncherPath(home)
	vbs, err := LauncherScript(entry)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(launcher, []byte("\ufeff"+vbs), atomicfile.Options{Mode: 0o600}); err != nil {
		return err
	}
	runCommand, err := RunCommand(launcher)
	if err != nil {
		return err
	}
	state := State{
		Version:      StateVersion,
		CLI:          cli,
		Script:       entry.Script,
		CodexHome:    codexHome,
		BenesHome:    home,
		LauncherPath: launcher,
		RunValue:     RunValue(home),
		RunCommand:   runCommand,
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(StatePath(home), append(raw, '\n'), atomicfile.Options{Mode: 0o600})
}

func addRunValue(name, command string) error {
	cmd := exec.Command("reg.exe", "add", RunKey, "/v", name, "/t", "REG_SZ", "/d", command, "/f", "/reg:64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("add HKCU Run value: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func QueryStatus(home string) Status {
	if runtime.GOOS != "windows" {
		return Status{Supported: false, Summary: "unsupported on " + runtime.GOOS}
	}
	raw, err := os.ReadFile(StatePath(home))
	if err != nil {
		return Status{Supported: true, Installed: false, Summary: "not installed"}
	}
	var state State
	if json.Unmarshal(raw, &state) != nil || strings.TrimSpace(state.RunValue) == "" {
		return Status{Supported: true, Installed: false, Summary: "invalid tray-state.json"}
	}
	return Status{Supported: true, Installed: true, Summary: "installed", RunValue: state.RunValue}
}
