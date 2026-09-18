package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestAttachKiroAccountBindsTokenWithMatchingRouting(t *testing.T) {
	home := t.TempDir()
	raw := []byte(`{"kiro":{"activeAccountId":"arn:aws:codewhisperer:eu-west-1:123456789012:profile/b","accounts":[
		{"id":"arn:aws:codewhisperer:us-east-1:123456789012:profile/a","credential":{"access":"tok-a","refresh":"rt-a","profileArn":"arn:aws:codewhisperer:us-east-1:123456789012:profile/a","apiRegion":"us-east-1","authType":"kiro_desktop"}},
		{"id":"arn:aws:codewhisperer:eu-west-1:123456789012:profile/b","credential":{"access":"tok-b","refresh":"rt-b","profileArn":"arn:aws:codewhisperer:eu-west-1:123456789012:profile/b","apiRegion":"eu-west-1","authType":"kiro_desktop"}}
	]}}`)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	specs := []providerregistry.Spec{
		{ID: "kiro", Protocol: providerregistry.ProtocolKiro, Endpoint: "https://runtime.us-east-1.kiro.dev"},
		{ID: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions"},
	}
	attachKiroAccount(home, specs)
	if specs[0].APIKey != "tok-b" || specs[0].ProfileARN == "" || specs[0].APIRegion != "eu-west-1" || specs[0].Endpoint != "https://runtime.eu-west-1.kiro.dev" {
		t.Fatalf("active spec=%#v", specs[0])
	}
	if len(specs[0].KiroAccounts) != 2 {
		t.Fatalf("stored snapshots=%d want 2: %#v", len(specs[0].KiroAccounts), specs[0].KiroAccounts)
	}
	if specs[1].APIKey != "" || specs[1].ProfileARN != "" || len(specs[1].KiroAccounts) != 0 {
		t.Fatalf("unrelated spec received kiro snapshot: %#v", specs[1])
	}
}
