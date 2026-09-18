package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunTrayStatus(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(home, "tray-state.json"), []byte(`{"runValue":"BenesTray","runCommand":"benes"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	code := runTray(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home}, nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	if runtime.GOOS != "windows" && !strings.Contains(stdout.String(), "supported: false") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	if runtime.GOOS == "windows" && !strings.Contains(stdout.String(), "installed: true") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}
