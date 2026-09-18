package claudedesktop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFingerprintDeterministicAndSensitive(t *testing.T) {
	base := Projection{Command: "benes", Args: []string{"mcp", "serve"}}
	equivalent := Projection{Command: "benes", Args: []string{"mcp", "serve"}}
	if base.Fingerprint() != equivalent.Fingerprint() {
		t.Fatal("equivalent projections produced different fingerprints")
	}
	if base.Fingerprint() == "" || len(base.Fingerprint()) != 64 {
		t.Fatalf("fingerprint=%q", base.Fingerprint())
	}
	changedCommand := Projection{Command: "benes-other", Args: []string{"mcp", "serve"}}
	if base.Fingerprint() == changedCommand.Fingerprint() {
		t.Fatal("command change did not change the fingerprint")
	}
	changedArgs := Projection{Command: "benes", Args: []string{"mcp", "serve", "--port", "23100"}}
	if base.Fingerprint() == changedArgs.Fingerprint() {
		t.Fatal("argument change did not change the fingerprint")
	}
	reordered := Projection{Command: "benes", Args: []string{"serve", "mcp"}}
	if base.Fingerprint() == reordered.Fingerprint() {
		t.Fatal("argument reorder did not change the fingerprint")
	}
}

func TestCanonicalIsSortedAndAbsentArgsMatchEmptyArgs(t *testing.T) {
	projection := Projection{Command: "benes", Args: []string{"a", "b"}}
	if got := string(projection.Canonical()); got != `{"args":["a","b"],"command":"benes"}` {
		t.Fatalf("canonical=%s", got)
	}
	if (Projection{Command: "benes"}).Fingerprint() != (Projection{Command: "benes", Args: []string{}}).Fingerprint() {
		t.Fatal("absent args and empty args disagreed")
	}
}

func TestFingerprintCarriesNoSecret(t *testing.T) {
	const secret = "sk-ant-claude-desktop-secret-value"
	// A projection has no field a secret could travel in, so the only way a
	// secret could reach a fingerprint is through the encoding of the fields it
	// does have.
	projection := Projection{Command: "benes", Args: []string{"mcp"}}
	canonical := string(projection.Canonical())
	if strings.Contains(canonical, secret) {
		t.Fatal("canonical projection carried a secret")
	}
	if strings.Contains(projection.Fingerprint(), secret) {
		t.Fatal("fingerprint carried a secret")
	}
	// The canonical encoding is exactly two known keys, so no extra value such
	// as an environment map or credential can be smuggled alongside them.
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(projection.Canonical(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("canonical keys=%v", decoded)
	}
	for key := range decoded {
		if key != "command" && key != "args" {
			t.Fatalf("unexpected canonical key %q", key)
		}
	}
}

func TestProjectionValidation(t *testing.T) {
	if (Projection{}).Valid() {
		t.Fatal("empty command accepted")
	}
	if (Projection{Command: "   "}).Valid() {
		t.Fatal("blank command accepted")
	}
	if (Projection{Command: "benes", Args: []string{"a\x00b"}}).Valid() {
		t.Fatal("NUL argument accepted")
	}
	if !(Projection{Command: "benes"}).Valid() {
		t.Fatal("valid projection rejected")
	}
}

func TestManagedNativeProjectionIsAbsentToday(t *testing.T) {
	if ManagedNativeProjection() != nil {
		t.Fatal("Benes now ships an MCP runtime; this test and the refusal must be revised")
	}
	if !strings.Contains(RuntimeUnavailableMessage, "no Claude Desktop MCP runtime") {
		t.Fatalf("message=%q", RuntimeUnavailableMessage)
	}
}
