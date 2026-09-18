package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestMmxCommandPathAndUnsafe(t *testing.T) {
	if got := strings.Join(mmxCommandPath([]string{"--quiet", "text", "chat"}), " "); got != "text chat" {
		t.Fatalf("path=%q", got)
	}
	if mmxUnsafeOverride([]string{"text", "--api-key", "sk-x"}) != "--api-key" {
		t.Fatal("expected --api-key")
	}
}

func TestRunMmxRejectsNonText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runMmx([]string{"image", "gen"}, &stdout, &stderr, defaultCommandDependencies())
	if code != 2 || !strings.Contains(stderr.String(), "mmx text") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunMmxLaunchesTextWithIsolatedConfig(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := launchMmx
	t.Cleanup(func() { launchMmx = prev })
	var gotEnv []string
	launchMmx = func(name string, args []string, env []string) error {
		if name != "mmx" {
			t.Fatalf("name=%s", name)
		}
		gotEnv = append([]string{}, env...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runMmx([]string{"text", "chat"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if envValue(gotEnv, "MINIMAX_REGION") != "global" {
		t.Fatalf("env=%v", gotEnv)
	}
	if strings.Contains(strings.Join(gotEnv, "\n"), "sk-") {
		t.Fatalf("secret in env: %v", gotEnv)
	}
}

func TestMmxBridgeRewritesAnthropicPathAndPlaceholderKey(t *testing.T) {
	var seenPath, seenKey, seenAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenKey = r.Header.Get("X-Api-Key")
		seenAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(upstream.Close)
	bridge, err := startMmxTextBridge(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bridge.Close)
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+bridge.port+"/anthropic/v1/messages", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer sk-secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || seenPath != "/v1/messages" || seenKey != mmxLoopbackKey || seenAuth != "" {
		t.Fatalf("status=%d path=%s key=%s auth=%s", res.StatusCode, seenPath, seenKey, seenAuth)
	}
}
