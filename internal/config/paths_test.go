package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveHomePrefersBenesHome(t *testing.T) {
	home := t.TempDir()
	got, source, err := ResolveHome(PathOptions{
		HomeDir: home,
		Env: map[string]string{
			"BENES_HOME": filepath.Join(home, "explicit-benes"),
		},
	})
	if err != nil {
		t.Fatalf("ResolveHome(): %v", err)
	}
	if source != HomeSourceBenesEnv {
		t.Fatalf("source=%q", source)
	}
	if got != filepath.Join(home, "explicit-benes") {
		t.Fatalf("home=%q", got)
	}
}

func TestResolveHomeDefaultsToDotBenes(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".benes"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, source, err := ResolveHome(PathOptions{HomeDir: home})
	if err != nil {
		t.Fatalf("ResolveHome(): %v", err)
	}
	if source != HomeSourceDefault {
		t.Fatalf("source=%q", source)
	}
	if got != filepath.Join(home, ".benes") {
		t.Fatalf("home=%q", got)
	}
}

func TestExpandUserPathOnlyExpandsCurrentUserHome(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "users", "alice")
	if got := ExpandUserPath("~/state", home); got != filepath.Join(home, "state") {
		t.Fatalf("got=%q", got)
	}
	if got := ExpandUserPath("~other/state", home); got != "~other/state" {
		t.Fatalf("got=%q", got)
	}
}

func TestResolveCodexHomeExplicitMissingFailsClosed(t *testing.T) {
	_, err := ResolveCodexHome(CodexHomeOptions{
		HomeDir: t.TempDir(),
		Env:     map[string]string{"CODEX_HOME": filepath.Join(t.TempDir(), "missing")},
	})
	if err == nil {
		t.Fatal("ResolveCodexHome() accepted missing explicit CODEX_HOME")
	}
}

func TestResolveCodexHomeExplicitFileFailsClosed(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveCodexHome(CodexHomeOptions{HomeDir: root, Env: map[string]string{"CODEX_HOME": file}})
	if err == nil {
		t.Fatal("ResolveCodexHome() accepted a file")
	}
}

func TestResolveCodexHomeExplicitResolvesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows runners")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveCodexHome(CodexHomeOptions{HomeDir: root, Env: map[string]string{"CODEX_HOME": link}})
	if err != nil {
		t.Fatalf("ResolveCodexHome(): %v", err)
	}
	realDir, err = filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != realDir {
		t.Fatalf("got=%q want=%q", got, realDir)
	}
}

func TestResolveCodexHomeUsesDefaultWhenUnset(t *testing.T) {
	home := t.TempDir()
	got, err := ResolveCodexHome(CodexHomeOptions{HomeDir: home, Env: map[string]string{}})
	if err != nil {
		t.Fatalf("ResolveCodexHome(): %v", err)
	}
	if got != filepath.Join(home, ".codex") {
		t.Fatalf("got=%q", got)
	}
}

func TestPathSetUsesResolvedHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "benes")
	paths, err := ResolvePaths(PathOptions{HomeDir: t.TempDir(), Env: map[string]string{"BENES_HOME": home}})
	if err != nil {
		t.Fatalf("ResolvePaths(): %v", err)
	}
	if paths.Config != filepath.Join(home, "config.json") {
		t.Fatalf("config=%q", paths.Config)
	}
	if paths.PID != filepath.Join(home, "benes.pid") {
		t.Fatalf("pid=%q", paths.PID)
	}
	if paths.RuntimePort != filepath.Join(home, "runtime-port.json") {
		t.Fatalf("runtime=%q", paths.RuntimePort)
	}
}

func TestResolveCodexHomePropagatesStatFailure(t *testing.T) {
	sentinel := errors.New("permission denied")
	_, err := ResolveCodexHome(CodexHomeOptions{
		HomeDir: "/home/test",
		Env:     map[string]string{"CODEX_HOME": "/private/codex"},
		Stat:    func(string) (os.FileInfo, error) { return nil, sentinel },
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want sentinel", err)
	}
}

func TestResolveCodexHomeDiscoversSingleWindowsHomeInWSL(t *testing.T) {
	root := t.TempDir()
	usersRoot := filepath.Join(root, "c", "Users")
	windowsCodex := filepath.Join(usersRoot, "Alice", ".codex")
	if err := os.MkdirAll(windowsCodex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsCodex, "config.toml"), []byte("# codex"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveCodexHome(CodexHomeOptions{
		HomeDir:   filepath.Join(root, "linux-home"),
		Platform:  "linux",
		Release:   "5.15.90.1-microsoft-standard-WSL2",
		UsersRoot: usersRoot,
	})
	if err != nil {
		t.Fatalf("ResolveCodexHome(): %v", err)
	}
	real, _ := filepath.EvalSymlinks(windowsCodex)
	if got != real {
		t.Fatalf("got=%q want=%q", got, real)
	}
}

func TestResolveCodexHomeDoesNotGuessBetweenMultipleWSLHomes(t *testing.T) {
	root := t.TempDir()
	usersRoot := filepath.Join(root, "c", "Users")
	for _, user := range []string{"Alice", "Bob"} {
		codex := filepath.Join(usersRoot, user, ".codex")
		if err := os.MkdirAll(codex, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codex, "config.toml"), []byte("# codex"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	linuxHome := filepath.Join(root, "linux-home")
	got, err := ResolveCodexHome(CodexHomeOptions{
		HomeDir:   linuxHome,
		Platform:  "linux",
		Release:   "microsoft-standard-WSL2",
		UsersRoot: usersRoot,
	})
	if err != nil {
		t.Fatalf("ResolveCodexHome(): %v", err)
	}
	if got != filepath.Join(linuxHome, ".codex") {
		t.Fatalf("got=%q", got)
	}
}

func TestWSLAutomountRootHonorsConfig(t *testing.T) {
	got := wslAutomountRoot("[automount]\nroot = /windows/\n")
	if got != "/windows" {
		t.Fatalf("root=%q", got)
	}
}

func TestResolveCodexHomeUsesUSERPROFILEToDisambiguateWSL(t *testing.T) {
	root := t.TempDir()
	mount := filepath.Join(root, "mnt")
	usersRoot := filepath.Join(mount, "c", "Users")
	for _, user := range []string{"Alice", "Bob"} {
		codex := filepath.Join(usersRoot, user, ".codex")
		if err := os.MkdirAll(codex, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codex, "config.toml"), []byte("# codex"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ResolveCodexHome(CodexHomeOptions{
		HomeDir:   filepath.Join(root, "linux-home"),
		Env:       map[string]string{"USERPROFILE": `C:\Users\Bob`},
		Platform:  "linux",
		Release:   "microsoft WSL2",
		UsersRoot: usersRoot,
		WSLConf:   "[automount]\nroot = " + mount + "\n",
	})
	if err != nil {
		t.Fatalf("ResolveCodexHome(): %v", err)
	}
	real, _ := filepath.EvalSymlinks(filepath.Join(usersRoot, "Bob", ".codex"))
	if got != real {
		t.Fatalf("got=%q want=%q", got, real)
	}
}
