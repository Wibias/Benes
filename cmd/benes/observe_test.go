package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunObserveLogsRequiresLiveProxy(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"logs"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunObserveUsageReadsLocalFileWithoutListener(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	line := `{"timestamp":` + strconv.FormatInt(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC).UnixMilli(), 10) + `,"provider":"openai","model":"gpt-5","account":"acct-a","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		t.Fatalf("offline usage called listener method=%s url=%s", method, u)
		return 0, nil, nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"usage", "--start", "2026-08-20T00:00", "--end", "2026-08-22T00:00", "--tz", "UTC", "--provider", "openai", "--model", "gpt-5", "--account", "acct-a", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"requests":1`) || strings.Contains(stdout.String(), "apiKey") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunObserveUsagePrintsMarkdownAndRejectsReversedRange(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	line := `{"timestamp":` + strconv.FormatInt(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC).UnixMilli(), 10) + `,"provider":"openai","model":"gpt-5","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"usage", "--range", "all"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "| Date | Requests | Tokens |") || strings.Contains(stdout.String(), `"requests"`) {
		t.Fatalf("markdown stdout=%q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runObserve(context.Background(), []string{"usage", "--start", "2026-08-22", "--end", "2026-08-20"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "end must be after start") {
		t.Fatalf("reversed code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunLogsRebuildIndex(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runLogs(context.Background(), []string{"rebuild-index"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "indexed rows") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runLogs(context.Background(), []string{"index-status"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "caught up") {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunObserveStorageCodexLogsStatus(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"storage", "codex-logs"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "protection:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunObserveStoragePrintsStubbedReport(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(u, "/api/storage") {
			t.Fatalf("method=%s url=%s", method, u)
		}
		return 200, []byte(`{"codexHome":"/tmp/codex","total":{"bytes":5,"fileCount":1},"buckets":[]}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"storage", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"fileCount":1`) || strings.Contains(stdout.String(), "apiKey") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunObserveLogsPrintsStubbedEntries(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(u, "/api/logs") {
			t.Fatalf("method=%s url=%s", method, u)
		}
		return 200, []byte(`[{"id":"req-1","timestamp":"2026-01-01T00:00:00Z","status":200,"path":"/v1/models","durationMs":3}]`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"logs"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "/v1/models") || strings.Contains(stdout.String(), "apiKey") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunObserveLogsFollowRejectsJSON(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runObserve(context.Background(), []string{"logs", "--follow", "--json"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "--jsonl") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunObserveLogsFollowSkipsSeenIDs(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	calls := 0
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(u, "/api/logs") {
			t.Fatalf("method=%s url=%s", method, u)
		}
		calls++
		if calls == 1 {
			return 200, []byte(`[{"id":"req-1","timestamp":"2026-01-01T00:00:00Z","status":200,"path":"/v1/models","durationMs":3}]`), nil
		}
		return 200, []byte(`[{"id":"req-1","timestamp":"2026-01-01T00:00:00Z","status":200,"path":"/v1/models","durationMs":3},{"id":"req-2","timestamp":"2026-01-01T00:00:01Z","status":200,"path":"/v1/chat","durationMs":4}]`), nil
	}
	defer func() { accessDo = prev }()
	ctx, cancel := context.WithCancel(context.Background())
	deps.sleep = func(time.Duration) {
		if calls >= 2 {
			cancel()
		}
	}
	var stdout, stderr bytes.Buffer
	if code := runObserve(ctx, []string{"logs", "--follow"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Count(stdout.String(), "/v1/models") != 1 {
		t.Fatalf("duplicated seen id stdout=%q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "/v1/chat") {
		t.Fatalf("missing new row stdout=%q", stdout.String())
	}
	if strings.Contains(stderr.String(), "not implemented") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
