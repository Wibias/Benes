//go:build !windows

package requesthistory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestUnixRebuildReplaceWithOpenReader(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, dbName)
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := l.Append([]byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(home); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_new"); err != nil {
		t.Fatal(err)
	}
}
