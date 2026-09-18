package codexrestore

import (
	"strings"
	"testing"
)

func TestApplyInjectStampsCodexHarnessIdentityWithoutDroppingSurface(t *testing.T) {
	t.Parallel()

	got := applyInject("", "http://127.0.0.1:18080/v1")
	if !strings.Contains(got, `"X-Benes-Harness" = "codex"`) {
		t.Fatalf("missing harness identity:\n%s", got)
	}
	if !strings.Contains(got, `"X-Benes-Surface" = "codex"`) {
		t.Fatalf("lost existing surface header:\n%s", got)
	}
}
