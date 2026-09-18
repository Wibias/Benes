package integrations

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathsPiHonorsCodingAgentDir(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "pi-elsewhere")
	got, err := ResolvePaths("pi", home, map[string]string{"PI_CODING_AGENT_DIR": elsewhere})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(elsewhere, "models.json")
	if got.ConfigPath != want {
		t.Fatalf("config=%q want=%q", got.ConfigPath, want)
	}
	if got.DetectDir != elsewhere {
		t.Fatalf("detect=%q want=%q", got.DetectDir, elsewhere)
	}
}

func TestResolvePathsPiKeepsHomeDefaultWithoutOverride(t *testing.T) {
	home := t.TempDir()
	got, err := ResolvePaths("pi", home, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigPath != filepath.Join(home, ".pi", "agent", "models.json") {
		t.Fatalf("config=%q", got.ConfigPath)
	}
	if got.DetectDir != filepath.Join(home, ".pi") {
		t.Fatalf("detect=%q", got.DetectDir)
	}
}

func TestResolvePathsPiRejectsRelativeCodingAgentDir(t *testing.T) {
	home := t.TempDir()
	_, err := ResolvePaths("pi", home, map[string]string{"PI_CODING_AGENT_DIR": "pi-elsewhere"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolvePathsPrimeHonorsCodingAgentDir(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "prime-elsewhere")
	got, err := ResolvePaths("prime", home, map[string]string{"PRIME_AGENT_CODING_AGENT_DIR": elsewhere})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(elsewhere, "models.json")
	if got.ConfigPath != want {
		t.Fatalf("config=%q want=%q", got.ConfigPath, want)
	}
	if got.DetectDir != elsewhere {
		t.Fatalf("detect=%q want=%q", got.DetectDir, elsewhere)
	}
}

func TestResolvePathsPrimeKeepsHomeDefaultWithoutOverride(t *testing.T) {
	home := t.TempDir()
	got, err := ResolvePaths("prime", home, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigPath != filepath.Join(home, ".prime", "agent", "models.json") {
		t.Fatalf("config=%q", got.ConfigPath)
	}
	if got.DetectDir != filepath.Join(home, ".prime") {
		t.Fatalf("detect=%q", got.DetectDir)
	}
}

func TestResolvePathsPrimeRejectsRelativeCodingAgentDir(t *testing.T) {
	home := t.TempDir()
	_, err := ResolvePaths("prime", home, map[string]string{"PRIME_AGENT_CODING_AGENT_DIR": "prime-elsewhere"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolvePathsPrimeAndPiKeepSeparateHomes(t *testing.T) {
	home := t.TempDir()
	pi, err := ResolvePaths("pi", home, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	prime, err := ResolvePaths("prime", home, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if pi.ConfigPath == prime.ConfigPath || pi.DetectDir == prime.DetectDir {
		t.Fatalf("pi=%+v prime=%+v", pi, prime)
	}
}
