package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/export"
)

func TestRunOpencodeInjectsRuntimeConfigWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BENES_API_AUTH_TOKEN", "sk-secret")
	t.Setenv(export.ConfigContentEnv, `{"provider":{"other":{"name":"keep"}}}`)
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(method, url string, body []byte) (int, []byte, error) {
		if !strings.Contains(url, "/api/client-config?client=opencode") {
			t.Fatalf("url=%s", url)
		}
		payload, _ := json.Marshal(map[string]any{
			"modelCount": 1,
			"text":       `{"provider":{"benes":{"name":"Benes","options":{"apiKey":"{env:BENES_OPENCODE_API_KEY}"}}}}`,
			"config": map[string]any{
				"provider": map[string]any{
					"benes": map[string]any{
						"name": "Benes",
						"options": map[string]any{
							"apiKey": "{env:" + export.OpenCodeAPIKeyEnv + "}",
						},
					},
				},
			},
		})
		return 200, payload, nil
	}
	prevLaunch := launchOpencode
	t.Cleanup(func() { launchOpencode = prevLaunch })
	var gotArgs []string
	var gotEnv []string
	launchOpencode = func(args []string, env []string) error {
		gotArgs = append([]string{}, args...)
		gotEnv = append([]string{}, env...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runOpencode([]string{"run", "prompt"}, &stdout, &stderr, commandDependencies{
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
	if strings.Join(gotArgs, " ") != "run prompt" {
		t.Fatalf("args=%v", gotArgs)
	}
	content := envValue(gotEnv, export.ConfigContentEnv)
	if !strings.Contains(content, "Benes") || !strings.Contains(content, `"other"`) || strings.Contains(content, "sk-secret") {
		t.Fatalf("content=%s", content)
	}
	if envValue(gotEnv, export.OpenCodeAPIKeyEnv) != "sk-secret" {
		t.Fatalf("env=%v", gotEnv)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
