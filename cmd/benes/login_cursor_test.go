package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/cursor"
)

type cursorRoundTrip func(*http.Request) (*http.Response, error)

func (f cursorRoundTrip) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestRunLoginCursorPollsAndPersistsWithoutPrintingTokens(t *testing.T) {
	home := t.TempDir()
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	payload, _ := json.Marshal(map[string]any{"sub": "user-1", "email": "ada@example.com", "exp": time.Now().Add(time.Hour).Unix()})
	token := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
	prevClient, prevAttempts, prevDelay := cursor.HTTPClient, cursor.MaxAttempts, cursor.BaseDelay
	t.Cleanup(func() {
		cursor.HTTPClient, cursor.MaxAttempts, cursor.BaseDelay = prevClient, prevAttempts, prevDelay
	})
	cursor.MaxAttempts = 2
	cursor.BaseDelay = 0
	cursor.HTTPClient = cursorRoundTrip(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]string{"accessToken": token, "refreshToken": token})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := runLogin(context.Background(), []string{"cursor"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cursor.com/loginDeepControl") || strings.Contains(stdout.String(), token) {
		t.Fatalf("stdout=%s", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"cursor"`) || !strings.Contains(string(raw), token) {
		t.Fatalf("auth=%s", raw)
	}
}
