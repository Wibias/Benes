package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestAttachAntigravityAccountsFromAuthStore(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"google-antigravity":{"access":"tok","refresh":"rt","expires":1,"projectId":"proj"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	specs := []providerregistry.Spec{{ID: "google-antigravity", Protocol: providerregistry.ProtocolGoogleAntigravity}}
	attachAntigravityAccounts(home, "", specs)
	if len(specs[0].Accounts) != 1 || specs[0].Accounts[0].Token != "tok" || specs[0].Accounts[0].ProjectID != "proj" {
		t.Fatalf("accounts=%#v", specs[0].Accounts)
	}
}

func TestLoadAntigravityAccountsIgnoresRelativeHome(t *testing.T) {
	if got := loadAntigravityAccounts("relative-must-not-be-read"); len(got) != 0 {
		t.Fatalf("relative=%#v", got)
	}
}
