package managedfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoProductionWritersDoNotBypassTheCoordinator(t *testing.T) {
	root := findModuleRoot(t)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/managedfs/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)
		if strings.Contains(body, "benes-managed.json") || strings.Contains(body, "benes-managed.lock") {
			t.Errorf("%s writes coordinator metadata outside managedfs", rel)
		}
		if strings.Contains(body, "os.WriteFile") && strings.Contains(body, "config.toml") {
			t.Errorf("%s writes config.toml outside the coordinator", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
