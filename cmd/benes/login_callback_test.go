package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/anthropic"
)

type anthRT func(*http.Request) (*http.Response, error)

func (f anthRT) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestRunLoginAnthropicCallbackPersistsWithoutPrintingTokens(t *testing.T) {
	home := t.TempDir()
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	prev := anthropic.HTTPClient
	t.Cleanup(func() { anthropic.HTTPClient = prev })
	anthropic.HTTPClient = anthRT(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret", "expires_in": 3600, "account": map[string]string{"uuid": "anth-1"}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := runLogin(context.Background(), []string{"anthropic"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("start code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "access-secret") || !strings.Contains(stdout.String(), "claude.ai") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runLogin(context.Background(), []string{"anthropic", "--callback", "abc"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("complete code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "access-secret") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"anthropic"`) || !strings.Contains(string(raw), "access-secret") {
		t.Fatalf("auth=%s", raw)
	}
}
