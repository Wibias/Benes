package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/runtimestate"
)

func TestRunHealthJSONReportsLiveProxy(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":42,"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runHealth([]string{"--json"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		inspectProcess: func(int) runtimestate.ProcessInfo {
			return runtimestate.ProcessInfo{Alive: true, Exe: "benes"}
		},
		probeListener: func(string, int) bool { return true },
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var payload struct {
		OK   bool `json:"ok"`
		PID  int  `json:"pid"`
		Port int  `json:"port"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.PID != 42 || payload.Port != 18080 {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestRunHealthExitsOneWhenStopped(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHealth(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: filepath.Join(t.TempDir(), "missing.json")}, nil
		},
	})
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout.String(), "Proxy not healthy") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunHealthRejectsStaleRuntimeFile(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":42,"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runHealth([]string{"--json"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
	})
	if code != 1 {
		t.Fatalf("code=%d stdout=%s", code, stdout.String())
	}
}
