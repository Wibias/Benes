package claudedesktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowsHomeEnv(home string) map[string]string {
	return map[string]string{
		"APPDATA":      filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
	}
}

func configPathForHome(home string) string {
	return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readConfigFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mustObservePath(t *testing.T, path string) Observed {
	t.Helper()
	native, err := ReadNative(path)
	if err != nil {
		t.Fatal(err)
	}
	return native.Observe()
}

func TestObservationKinds(t *testing.T) {
	cases := []struct {
		name string
		body string
		want ObservedKind
	}{
		{"no mcp servers", `{"preferences":{"theme":"dark"}}`, ObservedNoMCPServers},
		{"mcp servers not object", `{"mcpServers":["nope"]}`, ObservedMCPServersUnusable},
		{"mcp servers null", `{"mcpServers":null}`, ObservedMCPServersUnusable},
		{"no benes entry", `{"mcpServers":{"other":{"command":"x"}}}`, ObservedNoBenesEntry},
		{"benes not object", `{"mcpServers":{"benes":"x"}}`, ObservedEntryUnusable},
		{"benes without command", `{"mcpServers":{"benes":{"args":["a"]}}}`, ObservedEntryUnusable},
		{"benes empty command", `{"mcpServers":{"benes":{"command":"  "}}}`, ObservedEntryUnusable},
		{"benes command not string", `{"mcpServers":{"benes":{"command":7}}}`, ObservedEntryUnusable},
		{"benes extra key", `{"mcpServers":{"benes":{"command":"benes","args":[],"env":{}}}}`, ObservedEntryUnusable},
		{"benes args not strings", `{"mcpServers":{"benes":{"command":"benes","args":[1]}}}`, ObservedEntryUnusable},
		{"benes present", `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve"]}}}`, ObservedEntryPresent},
		{"benes present without args", `{"mcpServers":{"benes":{"command":"benes"}}}`, ObservedEntryPresent},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
			writeConfigFile(t, path, testCase.body)
			if got := mustObservePath(t, path).Kind; got != testCase.want {
				t.Fatalf("kind=%q want=%q", got, testCase.want)
			}
		})
	}
}

func TestObserveReportsConfigAbsentWithoutError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	native, err := ReadNative(path)
	if err != nil {
		t.Fatal(err)
	}
	if native.Exists() {
		t.Fatal("absent config reported as existing")
	}
	if got := native.Observe().Kind; got != ObservedNoMCPServers {
		t.Fatalf("kind=%q", got)
	}
}

func TestReadNativeFailsClosedOnNonObjectDocuments(t *testing.T) {
	for name, body := range map[string]string{
		"empty":            "",
		"whitespace":       "   \n\t",
		"null":             "null",
		"array":            "[1,2,3]",
		"scalar":           "\"text\"",
		"truncated object": `{"mcpServers":`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
			writeConfigFile(t, path, body)
			_, err := ReadNative(path)
			if !errors.Is(err, ErrNativeMalformed) {
				t.Fatalf("err=%v want ErrNativeMalformed", err)
			}
		})
	}
}

func TestReadNativeRefusesNonRegularTargets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadNative(dir); !errors.Is(err, ErrNativeNotRegular) {
		t.Fatalf("directory err=%v", err)
	}
	link := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere.json"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ReadNative(link); !errors.Is(err, ErrNativeNotRegular) {
		t.Fatalf("symlink err=%v", err)
	}
}

func TestDecodedEntryKeepsArgumentSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	writeConfigFile(t, path, `{"mcpServers":{"benes":{"command":"benes","args":["b","a"]}}}`)
	observed := mustObservePath(t, path)
	if observed.Entry == nil {
		t.Fatal("entry missing")
	}
	if !observed.Entry.Equal(Projection{Command: "benes", Args: []string{"b", "a"}}) {
		t.Fatalf("entry=%+v", *observed.Entry)
	}
	if observed.Entry.Equal(Projection{Command: "benes", Args: []string{"a", "b"}}) {
		t.Fatal("argument order was ignored")
	}
}

func TestEncodeDocumentPreservesUnrelatedRawValues(t *testing.T) {
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(`{"zeta":{"keep":[1, 2, 3]},"alpha":"plain","mcpServers":{"other":{"command":"o"}}}`), &root); err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"zeta": {"keep":[1, 2, 3]}`) {
		t.Fatalf("unrelated value was re-encoded:\n%s", text)
	}
	if !strings.Contains(text, `"mcpServers": {"other":{"command":"o"}}`) {
		t.Fatalf("unrelated server was re-encoded:\n%s", text)
	}
	if strings.Index(text, `"alpha"`) > strings.Index(text, `"mcpServers"`) {
		t.Fatalf("root keys were not sorted:\n%s", text)
	}
}
