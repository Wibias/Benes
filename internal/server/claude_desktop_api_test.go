package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const claudeDesktopSecret = "sk-ant-api03-desktop-secret"

// claudeDesktopTestHome isolates the runtime contract from the real host. The
// contract reads the process environment to resolve native locations, so the
// test owns the home directory and the Windows application data variables. No
// test reaches the developer's real Claude Desktop configuration.
func claudeDesktopTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	return home
}

func claudeDesktopNativeConfigPath(home string) string {
	return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
}

func claudeDesktopTestHandler(t *testing.T, configPath, home string) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		Home:           home,
		// The contract is host specific, so the test pins the platform instead
		// of inheriting whichever operating system runs the suite.
		Host: "windows",
	})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h)
}

func claudeDesktopRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func claudeDesktopDecode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var decoded T
	if err := json.Unmarshal(rr.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %s: %v", rr.Body.String(), err)
	}
	return decoded
}

// claudeDesktopStatusBody is the subset of the canonical response these tests
// assert on.
type claudeDesktopStatusBody struct {
	ClientID                   string                `json:"clientId"`
	HostSupported              bool                  `json:"hostSupported"`
	Installed                  bool                  `json:"installed"`
	Configurable               bool                  `json:"configurable"`
	ConfigPath                 string                `json:"configPath"`
	State                      string                `json:"state"`
	ManagedProjectionAvailable bool                  `json:"managedProjectionAvailable"`
	DesiredEnabled             bool                  `json:"desiredEnabled"`
	ObservedKind               string                `json:"observedKind"`
	Applied                    bool                  `json:"applied"`
	Stale                      bool                  `json:"stale"`
	RestartRequired            bool                  `json:"restartRequired"`
	DesiredFingerprint         string                `json:"desiredFingerprint"`
	AppliedFingerprint         string                `json:"appliedFingerprint"`
	ObservedFingerprint        string                `json:"observedFingerprint"`
	Refusal                    *claudeDesktopRefusal `json:"refusal"`
}

type claudeDesktopRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type claudeDesktopErrorEnvelope struct {
	Error claudeDesktopRefusal `json:"error"`
}

func claudeDesktopStatus(t *testing.T, h http.Handler) claudeDesktopStatusBody {
	t.Helper()
	rr := claudeDesktopRequest(h, http.MethodGet, "/api/claude-desktop/status", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	return claudeDesktopDecode[claudeDesktopStatusBody](t, rr)
}

func TestClaudeDesktopAPIOmitsSecrets(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"`+claudeDesktopSecret+`"}],"claudeDesktop":{"apiKey":"`+claudeDesktopSecret+`","model":"claude"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	native := claudeDesktopNativeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte(`{"preferences":{"note":"`+claudeDesktopSecret+`"},"mcpServers":{"other":{"command":"x","env":{"KEY":"`+claudeDesktopSecret+`"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)

	blocked := httptest.NewRequest(http.MethodGet, "/api/claude-desktop", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	for _, path := range []string{"/api/claude-desktop", "/api/claude-desktop/status"} {
		rr := claudeDesktopRequest(h, http.MethodGet, path, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), claudeDesktopSecret) {
			t.Fatalf("path=%s leaked a secret: %s", path, rr.Body.String())
		}
	}
}

func TestClaudeDesktopStatusIsTruthfulWithoutARuntime(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeDesktopEnabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	native := claudeDesktopNativeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte(`{"coworkUserFilesPath":"/home/user/Claude"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	status := claudeDesktopStatus(t, h)
	if status.ClientID != "claude-desktop" {
		t.Fatalf("clientId=%q", status.ClientID)
	}
	if !status.HostSupported || !status.Installed || !status.Configurable {
		t.Fatalf("status=%+v", status)
	}
	if status.ConfigPath != native {
		t.Fatalf("configPath=%q want=%q", status.ConfigPath, native)
	}
	if status.State != "no_managed_projection" || status.ManagedProjectionAvailable {
		t.Fatalf("status=%+v", status)
	}
	// Desired enablement is stored, and it is still not applied state.
	if !status.DesiredEnabled {
		t.Fatalf("desiredEnabled=%v", status.DesiredEnabled)
	}
	if status.Applied || status.Stale || status.RestartRequired {
		t.Fatalf("status claimed native state: %+v", status)
	}
	if status.DesiredFingerprint != "" || status.AppliedFingerprint != "" || status.ObservedFingerprint != "" {
		t.Fatalf("status invented fingerprints: %+v", status)
	}
	if status.Refusal == nil || status.Refusal.Code != "benes_mcp_runtime_unavailable" {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
}

func TestClaudeDesktopStatusNotInstalledWhenNoEvidenceExists(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	status := claudeDesktopStatus(t, h)
	if status.State != "not_installed" || status.Installed || status.Configurable {
		t.Fatalf("status=%+v", status)
	}
	if status.Refusal == nil || status.Refusal.Code != "not_installed" {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
}

func TestClaudeDesktopPUTStoresDesiredStateOnly(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
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
	rr := claudeDesktopRequest(h, http.MethodPut, "/api/claude-desktop", `{"enabled":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	status := claudeDesktopDecode[claudeDesktopStatusBody](t, rr)
	if !status.DesiredEnabled || status.Applied {
		t.Fatalf("status=%+v", status)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "claudeDesktopEnabled") {
		t.Fatalf("desired state was not stored: %s", raw)
	}
	// Storing a preference never writes native configuration.
	after, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("desired-state write mutated native configuration: %s", after)
	}
}

func TestClaudeDesktopPUTRefusesLegacyProfileWithoutEchoingSecrets(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	rr := claudeDesktopRequest(h, http.MethodPut, "/api/claude-desktop",
		`{"profile":{"model":"claude","assignments":{"opus":"x"},"appliedAt":"2026-01-01T00:00:00Z","apiKey":"`+claudeDesktopSecret+`"}}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), claudeDesktopSecret) {
		t.Fatalf("refusal echoed a secret: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "unsupported_field") {
		t.Fatalf("body=%s", rr.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "claudeDesktop") {
		t.Fatalf("refused write stored a profile: %s", raw)
	}
}

func TestClaudeDesktopPUTRejectsMalformedBodies(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	for _, body := range []string{`{}`, `{"enabled":true,"extra":1}`, `{"enabled":"yes"}`, `not json`} {
		rr := claudeDesktopRequest(h, http.MethodPut, "/api/claude-desktop", body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, rr.Code)
		}
	}
}

func TestClaudeDesktopApplyRefusesAndLeavesNativeConfigurationAlone(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeDesktopEnabled":true}`), 0o600); err != nil {
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
	rr := claudeDesktopRequest(h, http.MethodPost, "/api/claude-desktop/apply", "")
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	envelope := claudeDesktopDecode[claudeDesktopErrorEnvelope](t, rr)
	if envelope.Error.Code != "benes_mcp_runtime_unavailable" {
		t.Fatalf("error=%+v", envelope.Error)
	}
	after, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("refused apply mutated native configuration: %s", after)
	}
}

func TestClaudeDesktopDisableIsAnIdempotentNoOp(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeDesktopEnabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	native := claudeDesktopNativeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	before := `{"mcpServers":{"other":{"command":"x"}},"coworkUserFilesPath":"/home/user/Claude"}`
	if err := os.WriteFile(native, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	for attempt := 0; attempt < 2; attempt++ {
		rr := claudeDesktopRequest(h, http.MethodPost, "/api/claude-desktop/disable", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), `"changed":true`) {
			t.Fatalf("attempt=%d reported a change: %s", attempt, rr.Body.String())
		}
	}
	after, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("no-op disable mutated native configuration: %s", after)
	}
}

func TestClaudeDesktopStatusDoesNotAdoptAStoredLegacyProfile(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	legacy := `{"claudeDesktop":{"version":1,"model":"claude","assignments":{"opus":"a"},"defaults":{"opus":"a"},"appliedFingerprint":"deadbeef","appliedAt":"2026-01-01T00:00:00Z","apiKey":"` + claudeDesktopSecret + `"}}`
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	native := claudeDesktopNativeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	rr := claudeDesktopRequest(h, http.MethodGet, "/api/claude-desktop/status", "")
	body := rr.Body.String()
	for _, token := range []string{claudeDesktopSecret, `"appliedFingerprint"`, `"appliedAt"`, `"assignments"`, `"defaults"`, `"model"`, `"activeProfile"`} {
		if strings.Contains(body, token) {
			t.Fatalf("stored legacy profile leaked %q: %s", token, body)
		}
	}
	status := claudeDesktopDecode[claudeDesktopStatusBody](t, rr)
	if status.DesiredEnabled {
		t.Fatalf("stored profile implied desired enablement: %+v", status)
	}
	if status.Applied || status.DesiredFingerprint != "" || status.AppliedFingerprint != "" {
		t.Fatalf("stored profile implied applied state: %+v", status)
	}
	if status.State != "no_managed_projection" {
		t.Fatalf("state=%q", status.State)
	}
}

func TestClaudeDesktopMutationRoutesRejectNonPost(t *testing.T) {
	home := claudeDesktopTestHome(t)
	configPath := filepath.Join(home, "benes-config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := claudeDesktopTestHandler(t, configPath, home)
	for _, path := range []string{"/api/claude-desktop/apply", "/api/claude-desktop/disable"} {
		rr := claudeDesktopRequest(h, http.MethodGet, path, "")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d", path, rr.Code)
		}
	}
}
