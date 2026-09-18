package wintray

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValueIsStableAndPrefixed(t *testing.T) {
	got := RunValue(`C:\Users\Ada\.benes`)
	if !strings.HasPrefix(got, "BenesTray-") || len(got) != len("BenesTray-")+12 {
		t.Fatalf("runValue=%q", got)
	}
	if RunValue(`C:\Users\Ada\.benes\`) != got {
		t.Fatal("trailing slash changed the Run value")
	}
}

func TestProcessArgsPassCliPath(t *testing.T) {
	args, err := ProcessArgs(Entry{
		CLI: `C:\benes.exe`, Script: `C:\benes-tray.ps1`,
		CodexHome: `C:\codex`, BenesHome: `C:\benes`,
	}, "Run", 9)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `-CliPath C:\benes.exe`) || !strings.Contains(joined, "-HostPid 9") {
		t.Fatalf("args=%v", args)
	}
}

func TestEmbeddedScriptExecsCliPath(t *testing.T) {
	body, err := EmbeddedScript()
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "$psi.FileName = $CliPath") {
		t.Fatal("script does not exec the Go CLI")
	}
}

func TestEmbeddedIcons(t *testing.T) {
	icons, err := EmbeddedIcons()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"benes-tray-online.ico",
		"benes-tray-warning.ico",
		"benes-tray-offline.ico",
	} {
		if len(icons[name]) < 100 {
			t.Fatalf("icon %s missing", name)
		}
	}
}

func TestPersistWritesGoOwnedLayout(t *testing.T) {
	home := t.TempDir()
	cli := filepath.Join(home, "benes.exe")
	if err := persist(home, cli, filepath.Join(home, "codex")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ScriptPath(home)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StatePath(home)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LauncherPath(home)); err != nil {
		t.Fatal(err)
	}
}
