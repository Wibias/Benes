package claudedesktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const userConfigFixture = `{
  "preferences": {
    "theme": "dark",
    "nested": [1, 2, 3]
  },
  "coworkUserFilesPath": "/home/user/Claude",
  "mcpServers": {
    "other": {
      "command": "other-server",
      "args": ["--flag"]
    }
  }
}`

var managedFixture = Projection{Command: "benes", Args: []string{"mcp", "serve"}}

func decodeDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(readConfigFile(t, path)), &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	return doc
}

func managedEntry(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers=%v", doc["mcpServers"])
	}
	entry, ok := servers[ManagedEntryName].(map[string]any)
	if !ok {
		t.Fatalf("benes entry=%v", servers[ManagedEntryName])
	}
	return entry
}

func assertUnrelatedPreserved(t *testing.T, path string) {
	t.Helper()
	text := readConfigFile(t, path)
	if !strings.Contains(text, `"coworkUserFilesPath": "/home/user/Claude"`) {
		t.Fatalf("unrelated scalar lost its original bytes:\n%s", text)
	}
	// The nested user value keeps the source formatting it was read with.
	if !strings.Contains(text, `"theme": "dark"`) || !strings.Contains(text, `"nested": [1, 2, 3]`) {
		t.Fatalf("unrelated structure was re-encoded:\n%s", text)
	}
	doc := decodeDocument(t, path)
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers=%v", doc["mcpServers"])
	}
	other, ok := servers["other"].(map[string]any)
	if !ok {
		t.Fatalf("unrelated server lost: %v", servers)
	}
	if other["command"] != "other-server" {
		t.Fatalf("unrelated server changed: %v", other)
	}
}

func TestInstallPreservesUnrelatedConfigurationAndServers(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, userConfigFixture)

	result, err := InstallNative(path, managedFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Applied || !result.RestartRequired {
		t.Fatalf("result=%+v", result)
	}
	if result.Fingerprint != managedFixture.Fingerprint() {
		t.Fatalf("fingerprint=%q", result.Fingerprint)
	}
	assertUnrelatedPreserved(t, path)
	entry := managedEntry(t, decodeDocument(t, path))
	if entry["command"] != "benes" {
		t.Fatalf("entry=%v", entry)
	}
	args, _ := entry["args"].([]any)
	if len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Fatalf("args=%v", entry["args"])
	}
	if got := mustObservePath(t, path); got.Kind != ObservedEntryPresent || !got.Entry.Equal(managedFixture) {
		t.Fatalf("observed=%+v", got)
	}
}

func TestInstallIsIdempotentWithoutChurn(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, userConfigFixture)
	if _, err := InstallNative(path, managedFixture); err != nil {
		t.Fatal(err)
	}
	before := readConfigFile(t, path)
	result, err := InstallNative(path, managedFixture)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("an already-correct projection was rewritten")
	}
	if !result.Applied {
		t.Fatal("an already-correct projection was not reported applied")
	}
	if result.RestartRequired {
		t.Fatal("an unchanged file should not require a restart")
	}
	if after := readConfigFile(t, path); after != before {
		t.Fatalf("idempotent install churned the file:\n%s\n---\n%s", before, after)
	}
}

func TestInstallReplacesProjectionWhenDesiredChanges(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, userConfigFixture)
	if _, err := InstallNative(path, managedFixture); err != nil {
		t.Fatal(err)
	}
	next := Projection{Command: "benes", Args: []string{"mcp", "serve", "--port", "23101"}}
	result, err := InstallNative(path, next)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Fingerprint != next.Fingerprint() {
		t.Fatalf("result=%+v", result)
	}
	observed := mustObservePath(t, path)
	if observed.Entry == nil || !observed.Entry.Equal(next) {
		t.Fatalf("observed=%+v", observed)
	}
	if observed.Entry.Fingerprint() == managedFixture.Fingerprint() {
		t.Fatal("projection fingerprint did not change")
	}
	assertUnrelatedPreserved(t, path)
}

func TestInstallRefusesUnsafeDocumentsWithoutWriting(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"malformed", `{"mcpServers":`, ErrNativeMalformed},
		{"array root", `[1,2]`, ErrNativeMalformed},
		{"blocked container", `{"mcpServers":["nope"]}`, ErrNativeBlocked},
		{"foreign entry", `{"mcpServers":{"benes":{"command":"mine","args":[],"env":{"A":"1"}}}}`, ErrForeignEntry},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := configPathForHome(t.TempDir())
			writeConfigFile(t, path, testCase.body)
			before := readConfigFile(t, path)
			result, err := InstallNative(path, managedFixture)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err=%v want=%v", err, testCase.want)
			}
			if result.Changed || result.Applied {
				t.Fatalf("refused install reported success: %+v", result)
			}
			if after := readConfigFile(t, path); after != before {
				t.Fatalf("refused install rewrote the file:\n%s", after)
			}
		})
	}
}

func TestInstallRefusesInvalidProjection(t *testing.T) {
	path := configPathForHome(t.TempDir())
	if _, err := InstallNative(path, Projection{Command: "  "}); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("refused install created a file: %v", statErr)
	}
}

func TestRemoveExactManagedEntryPreservesEverythingElse(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, userConfigFixture)
	if _, err := InstallNative(path, managedFixture); err != nil {
		t.Fatal(err)
	}
	result, err := RemoveNative(path, managedFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Applied || !result.RestartRequired {
		t.Fatalf("result=%+v", result)
	}
	observed := mustObservePath(t, path)
	if observed.Kind != ObservedNoBenesEntry {
		t.Fatalf("observed=%+v", observed)
	}
	assertUnrelatedPreserved(t, path)
	// Removing twice is an unchanged no-op.
	again, err := RemoveNative(path, managedFixture)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed {
		t.Fatalf("second removal churned the file: %+v", again)
	}
}

func TestRemoveDropsEmptyManagedContainer(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, `{"coworkUserFilesPath": "/home/user/Claude"}`)
	if _, err := InstallNative(path, managedFixture); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveNative(path, managedFixture); err != nil {
		t.Fatal(err)
	}
	doc := decodeDocument(t, path)
	if _, ok := doc[mcpServersKey]; ok {
		t.Fatalf("empty container survived: %v", doc)
	}
	if doc["coworkUserFilesPath"] != "/home/user/Claude" {
		t.Fatalf("unrelated value lost: %v", doc)
	}
}

func TestRemoveRefusesMismatchedEntryWithoutWriting(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, `{"mcpServers":{"benes":{"command":"mine","args":["keep"]}}}`)
	before := readConfigFile(t, path)
	result, err := RemoveNative(path, managedFixture)
	if !errors.Is(err, ErrForeignEntry) {
		t.Fatalf("err=%v", err)
	}
	if result.Changed {
		t.Fatalf("refused removal reported a change: %+v", result)
	}
	if after := readConfigFile(t, path); after != before {
		t.Fatalf("refused removal rewrote the file:\n%s", after)
	}
}

func TestRemoveAbsentEntryLeavesFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	result, err := RemoveNative(path, managedFixture)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.RestartRequired {
		t.Fatalf("result=%+v", result)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("removal created a file: %v", statErr)
	}
}

func TestRemoveRefusesUnsafeDocuments(t *testing.T) {
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, `{"mcpServers":`)
	if _, err := RemoveNative(path, managedFixture); !errors.Is(err, ErrNativeMalformed) {
		t.Fatalf("err=%v", err)
	}
	blocked := configPathForHome(t.TempDir())
	writeConfigFile(t, blocked, `{"mcpServers":"no"}`)
	if _, err := RemoveNative(blocked, managedFixture); !errors.Is(err, ErrNativeBlocked) {
		t.Fatalf("blocked err=%v", err)
	}
}

func TestVerifyManagedEntryFailsWhenTheResultIsNotWhatWasAsked(t *testing.T) {
	dir := t.TempDir()
	mismatched := filepath.Join(dir, "mismatched.json")
	writeConfigFile(t, mismatched, `{"mcpServers":{"benes":{"command":"benes","args":["other"]}}}`)
	if err := verifyManagedEntry(mismatched, &managedFixture); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("mismatch err=%v", err)
	}
	present := filepath.Join(dir, "present.json")
	writeConfigFile(t, present, `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve"]}}}`)
	if err := verifyManagedEntry(present, nil); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("expected-absent err=%v", err)
	}
	if err := verifyManagedEntry(filepath.Join(dir, "missing.json"), nil); err != nil {
		t.Fatalf("absent-file err=%v", err)
	}
	if err := verifyManagedEntry(filepath.Join(dir, "missing.json"), &managedFixture); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("unverifiable err=%v", err)
	}
}

func TestWriteNativeFailsClosedWhenTheTargetCannotBePublished(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(blocker, "nested", "claude_desktop_config.json")
	root := map[string]json.RawMessage{mcpServersKey: json.RawMessage(`{"benes":{"command":"benes"}}`)}
	if err := writeNative(target, root, false); err == nil {
		t.Fatal("writeNative reported success for an unwritable target")
	}
	result, err := InstallNative(target, managedFixture)
	if err == nil {
		t.Fatalf("install reported success: %+v", result)
	}
	if result.Changed || result.Applied {
		t.Fatalf("failed install reported success: %+v", result)
	}
	// Nothing may be published. The parent is a file, so the platform error for
	// the target differs; reading it is the portable check.
	if raw, readErr := os.ReadFile(target); readErr == nil {
		t.Fatalf("failed install created a file: %s", raw)
	}
}

func TestInstallKeepsExistingFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := configPathForHome(t.TempDir())
	writeConfigFile(t, path, userConfigFixture)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallNative(path, managedFixture); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("permissions=%o want=640", perm)
	}
}
