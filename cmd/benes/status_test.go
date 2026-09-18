package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexrouting"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/runtimestate"
)

func isolateCodexHome(t *testing.T, configTOML string) {
	t.Helper()
	home := t.TempDir()
	if configTOML != "" {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(configTOML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", home)
}

func statusDeps(t *testing.T, runtimeJSON string) commandDependencies {
	t.Helper()
	home := t.TempDir()
	portPath := filepath.Join(home, "runtime-port.json")
	if runtimeJSON != "" {
		if err := os.WriteFile(portPath, []byte(runtimeJSON+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, RuntimePort: portPath}, nil
	}
	return deps
}

func withLiveRuntime(deps commandDependencies) commandDependencies {
	deps.inspectProcess = func(int) runtimestate.ProcessInfo {
		return runtimestate.ProcessInfo{Alive: true, Exe: "benes"}
	}
	deps.probeListener = func(string, int) bool { return true }
	return deps
}

func liveStatusDeps(t *testing.T, runtimeJSON string) commandDependencies {
	t.Helper()
	return withLiveRuntime(statusDeps(t, runtimeJSON))
}

func decodeStatusJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("json=%s err=%v", raw, err)
	}
	return got
}

func TestRunStatusReportsMissingProxy(t *testing.T) {
	isolateCodexHome(t, "")
	var stdout, stderr bytes.Buffer
	if code := runStatus(nil, &stdout, &stderr, statusDeps(t, "")); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "No running proxy found.") || !strings.Contains(got, "Codex route: unknown") {
		t.Fatalf("stdout=%q", got)
	}
}

func TestRunStatusPrintsPidPortAndHost(t *testing.T) {
	isolateCodexHome(t, "model_provider = \"openai\"\n")
	var stdout, stderr bytes.Buffer
	if code := runStatus(nil, &stdout, &stderr, liveStatusDeps(t, `{"pid":4242,"port":23100,"hostname":"127.0.0.1","exe":"benes"}`)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "PID 4242") || !strings.Contains(got, "port 23100") || !strings.Contains(got, "127.0.0.1") {
		t.Fatalf("stdout=%q", got)
	}
	if !strings.Contains(got, "Codex route: native") {
		t.Fatalf("stdout=%q", got)
	}
}

func TestRunStatusJSONReportsRunningProxy(t *testing.T) {
	isolateCodexHome(t, "model_provider = \"openai\"\n")
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--json"}, &stdout, &stderr, liveStatusDeps(t, `{"pid":7,"port":23100,"hostname":"127.0.0.1","exe":"benes"}`)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := decodeStatusJSON(t, stdout.String())
	if got["running"] != true || got["pid"] != float64(7) || got["codexRoute"] != "native" {
		t.Fatalf("%+v", got)
	}
}

func TestRunStatusJSONReportsCodexBenesFromConfigNotListener(t *testing.T) {
	isolateCodexHome(t, codexrouting.Marker+"\nmodel_provider = \"benes\"\n[model_providers.benes]\nbase_url = \"http://127.0.0.1:23100/v1\"\n")
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--json"}, &stdout, &stderr, liveStatusDeps(t, `{"pid":9,"port":23100,"hostname":"127.0.0.1","exe":"benes"}`)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := decodeStatusJSON(t, stdout.String())
	if got["codexRoute"] != "benes" || got["codexBaseUrl"] != "http://127.0.0.1:23100/v1" || got["codexManaged"] != true {
		t.Fatalf("%+v", got)
	}
	if got["port"] != float64(23100) {
		t.Fatalf("listener leaked into route: %+v", got)
	}
}

func TestRunStatusJSONCustomRemoteIsOther(t *testing.T) {
	isolateCodexHome(t, "openai_base_url = \"https://api.example.com/v1\"\n")
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--json"}, &stdout, &stderr, statusDeps(t, "")); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := decodeStatusJSON(t, stdout.String())
	if got["running"] != false || got["codexRoute"] != "other" || got["codexBaseUrl"] != "https://api.example.com/v1" {
		t.Fatalf("%+v", got)
	}
	if _, ok := got["pid"]; ok {
		t.Fatalf("stopped proxy should omit pid: %+v", got)
	}
}

func TestRunStatusOmitsUserinfoFromBaseURL(t *testing.T) {
	isolateCodexHome(t, "openai_base_url = \"http://user:secret@127.0.0.1:8080/v1\"\n")
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--json"}, &stdout, &stderr, statusDeps(t, "")); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := decodeStatusJSON(t, stdout.String())
	if got["codexRoute"] != "other" {
		t.Fatalf("%+v", got)
	}
	if _, ok := got["codexBaseUrl"]; ok {
		t.Fatalf("secret url leaked: %+v", got)
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunStatusJSONMissingCodexConfigIsUnknown(t *testing.T) {
	isolateCodexHome(t, "")
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--json"}, &stdout, &stderr, statusDeps(t, "")); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := decodeStatusJSON(t, stdout.String())
	if got["running"] != false || got["codexRoute"] != "unknown" || got["codexManaged"] != false {
		t.Fatalf("%+v", got)
	}
}

func TestRunStatusRejectsUnknownArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--pretty"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunStatusDoesNotTrustParsedRuntimeFileAlone(t *testing.T) {
	isolateCodexHome(t, "")
	var stdout, stderr bytes.Buffer
	if code := runStatus([]string{"--json"}, &stdout, &stderr, statusDeps(t, `{"pid":4242,"port":23100,"hostname":"127.0.0.1"}`)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := decodeStatusJSON(t, stdout.String())
	if got["running"] != false {
		t.Fatalf("stale file reported running: %+v", got)
	}
}

func TestRunStatusDoesNotTreatReusedPIDAsBenes(t *testing.T) {
	isolateCodexHome(t, "")
	deps := statusDeps(t, `{"pid":7,"port":23100,"hostname":"127.0.0.1","exe":"benes"}`)
	deps.inspectProcess = func(int) runtimestate.ProcessInfo {
		return runtimestate.ProcessInfo{Alive: true, Exe: "ping"}
	}
	deps.probeListener = func(string, int) bool { return true }
	var stdout, stderr bytes.Buffer
	if code := runStatus(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "another process") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
