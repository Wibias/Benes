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
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/kimi"
)

type kimiRT func(*http.Request) (*http.Response, error)

func (f kimiRT) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestRunLoginKimiPollsAndPersistsWithoutPrintingTokens(t *testing.T) {
	home := t.TempDir()
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	prevC, prevI := kimi.HTTPClient, kimi.Interval
	t.Cleanup(func() { kimi.HTTPClient, kimi.Interval = prevC, prevI })
	kimi.Interval = time.Nanosecond
	kimi.HTTPClient = kimiRT(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "device_authorization") {
			body, _ := json.Marshal(map[string]any{"user_code": "ABCD", "device_code": "dev-secret", "verification_uri": "https://auth.kimi.com/device", "expires_in": 600, "interval": 1})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
		}
		body, _ := json.Marshal(map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret", "expires_in": 3600})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := runLogin(context.Background(), []string{"kimi"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "access-secret") || strings.Contains(stdout.String(), "dev-secret") || !strings.Contains(stdout.String(), "ABCD") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"kimi"`) || !strings.Contains(string(raw), "access-secret") {
		t.Fatalf("auth=%s", raw)
	}
}
