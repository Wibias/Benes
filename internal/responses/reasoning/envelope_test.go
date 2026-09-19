package reasoning

import (
	"encoding/base64"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestEncodeMatchesTypeScriptWireFormat(t *testing.T) {
	got, err := Encode(Envelope{
		Signature:    "sig-1",
		Redacted:     []string{"red-a", "red-b"},
		Text:         "hidden",
		KiroRedacted: "kiro",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{"sig":"sig-1","red":["red-a","red-b"],"txt":"hidden","krc":"kiro"}`
	want := Prefix + base64.StdEncoding.EncodeToString([]byte(wantJSON))
	if got != want {
		t.Fatalf("wire mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestDecodeMatchesTypeScriptTolerance(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Envelope
		ok    bool
	}{
		{name: "native OpenAI blob is not Envelope", input: "opaque-native", ok: false},
		{name: "garbage payload", input: Prefix + "%%%", ok: false},
		{name: "array payload", input: Prefix + base64.StdEncoding.EncodeToString([]byte(`[]`)), ok: false},
		{name: "empty object", input: Prefix + base64.StdEncoding.EncodeToString([]byte(`{}`)), ok: false},
		{name: "trailing JSON is rejected like JSON.parse", input: Prefix + base64.StdEncoding.EncodeToString([]byte(`{"txt":"x"}{}`)), ok: false},
		{
			name:  "mixed redaction array keeps string entries",
			input: Prefix + base64.StdEncoding.EncodeToString([]byte(`{"sig":7,"red":["a",2,"",null,"b"],"txt":"","krc":"k"}`)),
			want:  Envelope{Redacted: []string{"a", "", "b"}, KiroRedacted: "k"},
			ok:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Decode(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v, envelope=%+v", ok, tt.ok, got)
			}
			if !tt.ok {
				return
			}
			if got.Signature != tt.want.Signature || got.Text != tt.want.Text || got.KiroRedacted != tt.want.KiroRedacted {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
			if len(got.Redacted) != len(tt.want.Redacted) {
				t.Fatalf("redacted=%q want %q", got.Redacted, tt.want.Redacted)
			}
			for i := range got.Redacted {
				if got.Redacted[i] != tt.want.Redacted[i] {
					t.Fatalf("redacted=%q want %q", got.Redacted, tt.want.Redacted)
				}
			}
		})
	}
}

func TestDecodeKiroKindCompatibilityAndValidation(t *testing.T) {
	legacy := Prefix + base64.StdEncoding.EncodeToString([]byte(`{"krc":"legacy"}`))
	got, ok := Decode(legacy)
	if !ok || got.KiroRedacted != "legacy" || got.KiroKind != protocol.KiroReasoningRedactedContent {
		t.Fatalf("legacy=%#v ok=%v", got, ok)
	}
	signature := Prefix + base64.StdEncoding.EncodeToString([]byte(`{"krc":"sig","krk":"signature"}`))
	got, ok = Decode(signature)
	if !ok || got.KiroRedacted != "sig" || got.KiroKind != protocol.KiroReasoningSignature {
		t.Fatalf("signature=%#v ok=%v", got, ok)
	}
	for _, raw := range []string{
		`{"krc":"x","krk":"unknown"}`,
		`{"krk":"signature"}`,
		`{"krc":"x","krk":7}`,
	} {
		encoded := Prefix + base64.StdEncoding.EncodeToString([]byte(raw))
		if got, ok := Decode(encoded); ok {
			t.Fatalf("malformed %s decoded as %#v", raw, got)
		}
	}
}

