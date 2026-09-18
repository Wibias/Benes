package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDashboardDirPrefersEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BENES_DASHBOARD_DIR", dir)
	if got := resolveDashboardDir(); got != dir {
		t.Fatalf("got=%q want=%q", got, dir)
	}
}
