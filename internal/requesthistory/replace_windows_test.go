//go:build windows

package requesthistory

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestRebuildBusyDestKeepsPrevious(t *testing.T) {
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
	ptr, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(ptr, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if _, err := Rebuild(home); err == nil {
		t.Fatal("expected busy dest to fail install")
	}
	windows.CloseHandle(handle)
	handle = windows.InvalidHandle
	if _, err := lookupOnly(home, "req_old"); err != nil {
		t.Fatalf("busy dest rebuild destroyed previous index: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}
