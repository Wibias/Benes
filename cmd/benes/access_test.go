package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func accessTestDeps(t *testing.T) commandDependencies {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	return deps
}

func TestRunAccessKeyCreateListRemove(t *testing.T) {
	deps := accessTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"key", "create", "ci"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("create code=%d stderr=%s", code, stderr.String())
	}
	created := stdout.String()
	if strings.Contains(created, "benes_data_") || !strings.Contains(created, "benes_") || !strings.Contains(created, "shown once") {
		t.Fatalf("create stdout=%q", created)
	}
	idLine := strings.Split(created, "(")
	if len(idLine) < 2 {
		t.Fatalf("missing id: %q", created)
	}
	id := strings.TrimSuffix(strings.Split(idLine[1], ")")[0], "")
	stdout.Reset()
	if code := runAccess([]string{"key", "list"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("list code=%d stderr=%s", code, stderr.String())
	}
	listed := stdout.String()
	if strings.Contains(listed, "benes_data_") || !strings.Contains(listed, "...") {
		t.Fatalf("list prefix=%q", listed)
	}
	if !strings.Contains(listed, id) || !strings.Contains(listed, "ci") {
		t.Fatalf("list stdout=%q", listed)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAPIKey([]string{"remove", id, "--yes"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runAccess([]string{"key", "list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "No API access keys configured.") {
		t.Fatalf("after remove stdout=%q", stdout.String())
	}
}

func TestRunAccessKeyRemoveRequiresYes(t *testing.T) {
	deps := accessTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"key", "remove", "missing"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "--yes") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunAccessKeysAliasLists(t *testing.T) {
	deps := accessTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"keys", "list"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No API access keys configured.") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunAccessModelsAndTestRequireLiveProxy(t *testing.T) {
	deps := accessTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"models"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("models code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := runAccess([]string{"test", "gpt-4o"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("test code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunAccessTestPostsChatWhenProxyLive(t *testing.T) {
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
	accessDo = func(method, url string, body []byte) (int, []byte, error) {
		if method != "POST" || !strings.Contains(url, "/v1/chat/completions") {
			t.Fatalf("method=%s url=%s", method, url)
		}
		if !strings.Contains(string(body), "gpt-4o") {
			t.Fatalf("body=%s", body)
		}
		return 200, []byte(`{"id":"ok"}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"test", "gpt-4o"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "succeeded") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunAccessModelsGetsLiveCatalog(t *testing.T) {
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
	accessDo = func(method, url string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(url, "/v1/models") {
			t.Fatalf("method=%s url=%s", method, url)
		}
		return 200, []byte(`{"data":[{"id":"gpt-4o","owned_by":"openai"}]}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"models"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "gpt-4o") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunAccessTestRejectsUnknownProtocol(t *testing.T) {
	deps := accessTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"test", "gpt-4o", "--protocol", "soap"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "protocol") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
