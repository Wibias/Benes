package claudedesktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func missLookPath(string) (string, error) { return "", errors.New("not found") }

func TestConfigPathPerSupportedHost(t *testing.T) {
	home := t.TempDir()
	roaming := filepath.Join(home, "AppData", "Roaming")
	env := map[string]string{"APPDATA": roaming}

	windows, err := ConfigPath("windows", home, env)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(roaming, "Claude", "claude_desktop_config.json")
	if windows != want {
		t.Fatalf("windows config=%q want=%q", windows, want)
	}
	mac, err := ConfigPath("darwin", home, env)
	if err != nil {
		t.Fatal(err)
	}
	wantMac := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	if mac != wantMac {
		t.Fatalf("macos config=%q want=%q", mac, wantMac)
	}
}

func TestUnsupportedHostFailsClosed(t *testing.T) {
	home := t.TempDir()
	for _, host := range []string{"linux", "freebsd", "plan9"} {
		if HostSupported(host) {
			t.Fatalf("%s reported as supported", host)
		}
		if _, err := ConfigPath(host, home, nil); !errors.Is(err, ErrUnsupportedHost) {
			t.Fatalf("%s config err=%v", host, err)
		}
		if candidates := ConfigCandidates(host, home, nil); len(candidates) != 0 {
			t.Fatalf("%s candidates=%v", host, candidates)
		}
		if evidence := DetectInstall(host, home, nil, func(string) (string, error) {
			return "/usr/local/bin/claude", nil
		}); evidence != "" {
			t.Fatalf("%s install evidence=%q", host, evidence)
		}
	}
}

func TestEmptyHostMeansRunningHost(t *testing.T) {
	supported := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if got := HostSupported("windows"); !got {
		t.Fatal("windows must be supported")
	}
	if got := HostSupported(""); got != supported {
		t.Fatalf("empty host=%v running host=%s supported=%v", got, runtime.GOOS, supported)
	}
}

func TestDetectInstallPrefersApplicationOverCommandAndDataDir(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "AppData", "Local")
	env := map[string]string{
		"APPDATA":      filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": local,
	}
	// Application data directory alone is the weakest evidence.
	dataDir := filepath.Join(home, "AppData", "Roaming", "Claude")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := DetectInstall("windows", home, env, missLookPath); got != dataDir {
		t.Fatalf("data dir evidence=%q want=%q", got, dataDir)
	}
	// A desktop command on PATH outranks the data directory.
	command := filepath.Join(home, "bin", "claude-desktop")
	if err := os.MkdirAll(filepath.Dir(command), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("shim"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := DetectInstall("windows", home, env, func(name string) (string, error) {
		if name != ClientID {
			return "", errors.New("unexpected name")
		}
		return command, nil
	}); got != command {
		t.Fatalf("command evidence=%q want=%q", got, command)
	}
	// The application executable outranks everything.
	exe := filepath.Join(local, "AnthropicClaude", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("MZ"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := DetectInstall("windows", home, env, func(string) (string, error) {
		return command, nil
	}); got != exe {
		t.Fatalf("executable evidence=%q want=%q", got, exe)
	}
}

func TestDetectInstallDoesNotTreatClaudeCodeCLIAsDesktop(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{
		"APPDATA":      filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
	}
	looked := []string{}
	evidence := DetectInstall("windows", home, env, func(name string) (string, error) {
		looked = append(looked, name)
		// Mimic a host where `claude` resolves to the Claude Code CLI.
		if name == "claude" {
			return filepath.Join(home, ".local", "bin", "claude.exe"), nil
		}
		return "", errors.New("not found")
	})
	if evidence != "" {
		t.Fatalf("desktop install claimed from %q", evidence)
	}
	for _, name := range looked {
		if strings.EqualFold(name, "claude") {
			t.Fatalf("desktop detection probed the Claude Code command name %q", name)
		}
	}
}

func TestDetectInstallWithoutEvidence(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{
		"APPDATA":      filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
	}
	if got := DetectInstall("darwin", home, env, missLookPath); got != "" {
		t.Fatalf("install evidence=%q", got)
	}
}
