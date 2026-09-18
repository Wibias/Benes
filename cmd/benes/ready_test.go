package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

func TestParseReadyArgsRejectsTimeoutWithoutWait(t *testing.T) {
	if _, ok := parseReadyArgs([]string{"--timeout", "5"}); ok {
		t.Fatal("expected usage failure")
	}
}

func TestRunReadyJSONWhenReadyzOK(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":21,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runReady([]string{"--json"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		probeReady: func(host string, port int) readyProbe {
			if host != "127.0.0.1" || port != 18080 {
				t.Fatalf("host=%s port=%d", host, port)
			}
			return readyProbe{Ready: true, Status: "ready", PID: 21, Port: 18080}
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var payload struct {
		Ready  bool   `json:"ready"`
		Status string `json:"status"`
		PID    int    `json:"pid"`
		Port   int    `json:"port"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Ready || payload.Status != "ready" || payload.PID != 21 || payload.Port != 18080 {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestRunReadyWaitTimesOutUnreachable(t *testing.T) {
	now := time.Unix(0, 0)
	var stdout, stderr bytes.Buffer
	code := runReady([]string{"--wait", "--timeout", "1", "--json"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: filepath.Join(t.TempDir(), "missing.json")}, nil
		},
		now: func() time.Time {
			current := now
			now = now.Add(600 * time.Millisecond)
			return current
		},
		sleep: func(time.Duration) {},
	})
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"unreachable"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunReadyUnknownFlagIsCode64(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runReady([]string{"--bogus"}, &stdout, &stderr, commandDependencies{}); code != 64 {
		t.Fatalf("code=%d", code)
	}
}
