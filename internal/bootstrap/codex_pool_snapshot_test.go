package bootstrap

import (
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
)

func TestBootstrapQuotaScopeMatchesNativeCodexGroups(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  codexauth.QuotaScope
	}{
		{model: "", want: ""},
		{model: "gpt-5.3-codex-spark", want: codexauth.QuotaScopeSpark},
		{model: "GPT-5.3-CODEX-SPARK", want: codexauth.QuotaScopeSpark},
		{model: "gpt-5.6", want: codexauth.QuotaScopeShared},
		{model: "other-native-model", want: codexauth.QuotaScopeShared},
	} {
		if got := bootstrapQuotaScope(tc.model); got != tc.want {
			t.Fatalf("model=%q scope=%q want=%q", tc.model, got, tc.want)
		}
	}
}
