package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func keysTestHandler(t *testing.T, configJSON string, options Options) (http.Handler, string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	options.ConfigPath = configPath
	if options.Providers == nil {
		options.Providers = map[string]Provider{"openai": &fakeProvider{}}
	}
	h, err := NewHandler(options)
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h, configPath
}

func TestKeysGETLoopbackListsPrefixWithoutSecret(t *testing.T) {
	h, _ := keysTestHandler(t, `{
		"apiKeys":[{"id":"k1","name":"ci","key":"benes_data_abcdefghijklmnopqrstuvwxyz","createdAt":"2026-01-02T00:00:00Z"}],
		"claudeCode":{"enabled":false}
	}`, Options{AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"}})

	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "benes_data_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("GET leaked secret: %s", rr.Body.String())
	}
	var body struct {
		Keys []struct {
			ID     string         `json:"id"`
			Name   string         `json:"name"`
			Prefix string         `json:"prefix"`
			Usage  map[string]any `json:"usage"`
		} `json:"keys"`
		AuthMatrix        []map[string]string `json:"authMatrix"`
		ClaudeCodeEnabled bool                `json:"claudeCodeEnabled"`
		BaseURL           string              `json:"baseUrl"`
		Endpoint          string              `json:"endpoint"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Keys) != 1 || body.Keys[0].ID != "k1" || body.Keys[0].Prefix != "benes_abcdef..." {
		t.Fatalf("keys=%#v", body.Keys)
	}
	if body.Keys[0].Usage["requests7d"] != float64(0) || body.Keys[0].Usage["totalRequests"] != float64(0) {
		t.Fatalf("usage=%#v", body.Keys[0].Usage)
	}
	if body.ClaudeCodeEnabled {
		t.Fatal("claudeCodeEnabled should follow config")
	}
	if body.BaseURL != "http://127.0.0.1:23100/v1" || body.Endpoint != "http://127.0.0.1:23100/v1/responses" {
		t.Fatalf("endpoints base=%q endpoint=%q", body.BaseURL, body.Endpoint)
	}
	if len(body.AuthMatrix) != 3 {
		t.Fatalf("matrix=%#v", body.AuthMatrix)
	}
	for _, row := range body.AuthMatrix {
		if row["bearer"] != "accepted" || row["dedicated"] != "accepted" || row["xApiKey"] != "accepted" {
			t.Fatalf("loopback matrix row=%#v", row)
		}
		if row["endpoint"] == "/v1/messages" {
			t.Fatal("messages must be omitted when Claude Code is off")
		}
	}
}

func TestKeysGETRemoteMatrixRequiresDedicatedHeader(t *testing.T) {
	h, _ := keysTestHandler(t, `{"apiKeys":[]}`, Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:    "0.0.0.0",
			DataPlaneTokens: []string{"remote-secret"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		AuthMatrix []map[string]string `json:"authMatrix"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.AuthMatrix) == 0 {
		t.Fatal("empty matrix")
	}
	row := body.AuthMatrix[0]
	if row["bearer"] != "rejected" || row["dedicated"] != "required" || row["xApiKey"] != "rejected" {
		t.Fatalf("remote matrix=%#v", row)
	}
}

func TestKeysCreateListDeleteRoundTrip(t *testing.T) {
	h, configPath := keysTestHandler(t, `{"providers":{}}`, Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
	})
	create := loopbackJSON(http.MethodPost, "/api/keys", `{"name":"ci"}`)
	createRR := httptest.NewRecorder()
	h.ServeHTTP(createRR, create)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || !strings.HasPrefix(created.Key, "benes_") || strings.HasPrefix(created.Key, "benes_data_") || len(created.Key) != 46 {
		t.Fatalf("created=%#v", created)
	}
	disk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(disk), created.Key) {
		t.Fatal("created key was not persisted")
	}

	list := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusOK || strings.Contains(listRR.Body.String(), created.Key) {
		t.Fatalf("list status=%d body=%s", listRR.Code, listRR.Body.String())
	}

	del := loopbackJSON(http.MethodDelete, "/api/keys", `{"id":"`+created.ID+`"}`)
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), `"apiKeys"`) {
		t.Fatalf("apiKeys remained after last delete: %s", after)
	}
}

func TestKeysDuplicateIDIsAmbiguous(t *testing.T) {
	h, _ := keysTestHandler(t, `{
		"apiKeys":[
			{"id":"dup","name":"a","key":"benes_data_aaaaaaaaaaaaaaaaaaaa"},
			{"id":"dup","name":"b","key":"benes_data_bbbbbbbbbbbbbbbbbbbb"}
		]
	}`, Options{AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"}})
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var body struct {
		Keys []struct {
			Usage map[string]any `json:"usage"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Keys) != 2 {
		t.Fatalf("keys=%#v", body.Keys)
	}
	for _, row := range body.Keys {
		if row.Usage["ambiguous"] != true {
			t.Fatalf("usage=%#v", row.Usage)
		}
	}
}

func TestKeysRejectsNonLoopbackHost(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestKeysPOSTRejectsOverlongName(t *testing.T) {
	h, _ := keysTestHandler(t, `{"providers":{}}`, Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
	})
	name := strings.Repeat("n", 65)
	req := loopbackJSON(http.MethodPost, "/api/keys", `{"name":"`+name+`"}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAPIKeyPrefixDropsDataSegment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key, want string
	}{
		{"benes_data_abcdefghijklmnopqrstuvwxyz", "benes_abcdef..."},
		{"benes_" + strings.Repeat("ab", 20), "benes_ababab..."},
		{"short", "short"},
		{"benes_abcdef", "benes_abcdef"},
	}
	for _, tc := range cases {
		if got := apiKeyPrefix(tc.key); got != tc.want {
			t.Errorf("apiKeyPrefix(%q)=%q want %q", tc.key, got, tc.want)
		}
	}
}
