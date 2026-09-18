package nativemain

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func jwt(account string) string {
	payload, _ := json.Marshal(map[string]any{"chatgpt_account_id": account})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func writeAuth(t *testing.T, home, account string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"id_token":      jwt(account),
			"access_token":  jwt(account),
			"refresh_token": "rt-" + account,
			"account_id":    account,
		},
	})
	if err := os.WriteFile(filepath.Join(home, "auth.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterListAndSwitch(t *testing.T) {
	codex := t.TempDir()
	cfg := t.TempDir()
	writeAuth(t, codex, "acct-a")
	mgr, err := NewManager(codex, cfg, NewMemoryKeyProvider())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Register("primary"); err != nil {
		t.Fatal(err)
	}
	listed, err := mgr.List()
	if err != nil || len(listed.Profiles) != 1 || listed.Profiles[0].Label != "primary" {
		t.Fatalf("%+v %v", listed, err)
	}
	stage, err := mgr.PrepareStage()
	if err != nil {
		t.Fatal(err)
	}
	home := stage["stagingCodexHome"].(string)
	writeAuth(t, home, "acct-b")
	if _, err := mgr.Finish(stage["stageId"].(string), stage["writerToken"].(string), "secondary"); err != nil {
		t.Fatal(err)
	}
	switched, err := mgr.Switch("secondary", true)
	if err != nil {
		t.Fatal(err)
	}
	active := switched["activeProfile"].(PublicProfile)
	if active.Label != "secondary" {
		t.Fatalf("%+v", switched)
	}
	got, _ := os.ReadFile(filepath.Join(codex, "auth.json"))
	if !contains(string(got), "acct-b") {
		t.Fatalf("auth not switched: %s", got)
	}
}
