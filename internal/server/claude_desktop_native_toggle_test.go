package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeDesktopNativeToggleOmitsSecretsAndNeverClaimsCurrent(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"`+claudeDesktopSecret+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	native := claudeDesktopNativeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	before := `{"coworkUserFilesPath":"/home/user/Claude"}`
	if err := os.WriteFile(native, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	rr := claudeDesktopRequest(h, http.MethodPut, "/api/native-integrations/claude-desktop", `{"enabled":true}`)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), claudeDesktopSecret) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, `"state":"current"`) {
		t.Fatalf("enable claimed applied native state: %s", body)
	}
	if !strings.Contains(body, `"state":"absent"`) {
		t.Fatalf("body=%s", body)
	}
	if !strings.Contains(body, "benes_mcp_runtime_unavailable") {
		t.Fatalf("refusal reason missing: %s", body)
	}
	after, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("toggle mutated native configuration: %s", after)
	}
}

type nativeStatusRow struct {
	ClientID       string `json:"clientId"`
	State          string `json:"state"`
	Installed      bool   `json:"installed"`
	ConfigPath     string `json:"configPath"`
	DesiredEnabled bool   `json:"desiredEnabled"`
}

type nativeStatusListEnvelope struct {
	Clients []nativeStatusRow `json:"clients"`
}

func TestNativeIntegrationsListReportsClaudeDesktopTruthfully(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeDesktopEnabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	native := claudeDesktopNativeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	rr := claudeDesktopRequest(h, http.MethodGet, "/api/native-integrations", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	list := claudeDesktopDecode[nativeStatusListEnvelope](t, rr)
	var found bool
	for _, row := range list.Clients {
		if row.ClientID != "claude-desktop" {
			continue
		}
		found = true
		if row.State != "absent" {
			t.Fatalf("state=%q", row.State)
		}
		if !row.Installed {
			t.Fatalf("installed=%v", row.Installed)
		}
		if row.ConfigPath != native {
			t.Fatalf("configPath=%q want=%q", row.ConfigPath, native)
		}
		if !row.DesiredEnabled {
			t.Fatalf("desiredEnabled=%v", row.DesiredEnabled)
		}
	}
	if !found {
		t.Fatalf("claude-desktop row missing: %s", rr.Body.String())
	}
}
