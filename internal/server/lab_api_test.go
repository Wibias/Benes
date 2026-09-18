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
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/lab"
)

func TestLabAPIProjectsCapabilityVerdicts(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4", DisplayName: "GPT-5.4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/lab/status", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/lab/status", nil)
	statusReq.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", statusRR.Code, statusRR.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(statusRR.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["projectionAvailable"] != true || status["projectionSpecVersion"] != "capability-v1" {
		t.Fatalf("status=%v", status)
	}
	verdictsReq := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts", nil)
	verdictsReq.Host = "127.0.0.1"
	verdictsRR := httptest.NewRecorder()
	h.ServeHTTP(verdictsRR, verdictsReq)
	if verdictsRR.Code != http.StatusOK {
		t.Fatalf("verdicts status=%d body=%s", verdictsRR.Code, verdictsRR.Body.String())
	}
	var page struct {
		Verdicts []struct {
			SubjectID     string `json:"subjectId"`
			SuiteID       string `json:"suiteId"`
			EvidenceLayer string `json:"evidenceLayer"`
			Verdict       string `json:"verdict"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal(verdictsRR.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Verdicts) == 0 {
		t.Fatal("expected capability verdicts")
	}
	if page.Verdicts[0].SubjectID != "openai-apikey/gpt-5.4" || page.Verdicts[0].EvidenceLayer != "live_route_compatibility" {
		t.Fatalf("verdict=%v", page.Verdicts[0])
	}
	community := httptest.NewRequest(http.MethodGet, "/api/lab/public/community", nil)
	community.Host = "127.0.0.1"
	communityRR := httptest.NewRecorder()
	h.ServeHTTP(communityRR, community)
	if communityRR.Code != http.StatusOK || !strings.Contains(communityRR.Body.String(), `"trustClass":"community_untrusted_v1"`) {
		t.Fatalf("community=%s", communityRR.Body.String())
	}
}

func TestLabAPIClaimedProtocolWithoutStore(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4", DisplayName: "GPT-5.4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts?layer=protocol_conformance", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"verdict":"CLAIMED"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestLabAPIOverlaysStoreVerdicts(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","models":["gpt-5.4"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadDiskConfig(configPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lab.Rebuild(filepath.Join(home, "lab"), disk, 11); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts?layer=protocol_conformance&suiteId=codex", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"verdict":"VERIFIED"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
	obs := httptest.NewRequest(http.MethodGet, "/api/lab/observations", nil)
	obs.Host = "127.0.0.1"
	obsRR := httptest.NewRecorder()
	h.ServeHTTP(obsRR, obs)
	if obsRR.Code != http.StatusOK || !strings.Contains(obsRR.Body.String(), `"executionMode":"local_classifier"`) {
		t.Fatalf("obs=%s", obsRR.Body.String())
	}
}

func TestLabAPICorruptLedgerKeepsDerivedLiveRoute(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "lab"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "lab", "events.jsonl"), []byte("nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	status := httptest.NewRequest(http.MethodGet, "/api/lab/status", nil)
	status.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, status)
	if !strings.Contains(statusRR.Body.String(), `"corruptionCount":1`) {
		t.Fatalf("status=%s", statusRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts?layer=live_route_compatibility", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"evidenceLayer":"live_route_compatibility"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
