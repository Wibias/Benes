package accountimport

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func TestParseRejectsNonArrayAndUnknownKeys(t *testing.T) {
	if _, code := ParseCockpitDocument(map[string]any{"accounts": []any{}}); code != "invalid_document" {
		t.Fatal(code)
	}
	records, code := ParseCockpitDocument([]any{map[string]any{"email": "user@example.com", "refresh_token": "rt", "extra": 1}})
	if code != "" || len(records) != 1 || !records[0].Invalid {
		t.Fatalf("%+v %s", records, code)
	}
}

func TestImportAdmitsProviderFormatBeforeValidate(t *testing.T) {
	called := false
	out := Import(context.Background(), "openai", Format, []any{}, t.TempDir()+"/auth.json", func(context.Context, string) (antigravity.ImportCredential, error) {
		called = true
		return antigravity.ImportCredential{}, nil
	})
	if out.Code != "unsupported_provider" || called {
		t.Fatalf("%+v called=%v", out, called)
	}
}
