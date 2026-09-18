package grok

import (
	"strings"
	"testing"
)

func TestBuildManagedBlockStampsGrokHarnessIdentityWithoutDroppingExistingHeaders(t *testing.T) {
	t.Parallel()

	got := BuildManagedBlock(18080, []Model{{ID: "xai/grok", Name: "Grok"}}, "127.0.0.1")
	for _, want := range []string{
		`"X-Benes-Harness" = "grok"`,
		`"x-benes-grok" = "1"`,
		`"X-Benes-Surface" = "grok"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("managed block missing %s:\n%s", want, got)
		}
	}
}
