package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestRoutingProfilesAPIIsLoopbackAndOmitsSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"alias":"fast-alias","candidates":[{"provider":"openai-apikey","model":"gpt-5.5"}],"apiKey":"sk-secret"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") || strings.Contains(rr.Body.String(), `"apiKey"`) {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Profiles []struct {
			ID    string `json:"id"`
			Alias string `json:"alias"`
			Model string `json:"model"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Profiles) != 1 || got.Profiles[0].ID != "fast" || got.Profiles[0].Alias != "fast-alias" || got.Profiles[0].Model != "policy/fast" {
		t.Fatalf("profiles=%v", got.Profiles)
	}
}

func TestRoutingProfilesAPIEmptyWithoutConfig(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"profiles"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestRoutingProfileDryRunSelectsFirstCandidate(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"candidates":[{"provider":"openai-apikey","model":"gpt-5.5"},{"provider":"xai","model":"grok-4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/routing-profiles/dry-run", strings.NewReader(`{"profile":"fast","evidence":{"toolsRequired":true}}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"selectedIndex":0`) || !strings.Contains(rr.Body.String(), `"gpt-5.5"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoutingProfileDryRunEmitsScores(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"candidates":[{"provider":"openai-apikey","model":"gpt-5.5"},{"provider":"xai","model":"grok-4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil), "xai": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/routing-profiles/dry-run", strings.NewReader(`{"profile":"fast","evidence":{}}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"score"`) || !strings.Contains(rr.Body.String(), `"total"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoutingAnalyticsCountsPolicyWalks(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/fast","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("responses status=%d body=%s", rr.Code, rr.Body.String())
	}
	analytics := httptest.NewRequest(http.MethodGet, "/api/routing-analytics", nil)
	analytics.Host = "127.0.0.1"
	got := httptest.NewRecorder()
	h.ServeHTTP(got, analytics)
	if got.Code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", got.Code, got.Body.String())
	}
	var payload struct {
		TotalRequests int `json:"totalRequests"`
		DurationMs    struct {
			SampleCount int `json:"sampleCount"`
		} `json:"durationMs"`
		Breakdown []struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
			Requests int    `json:"requests"`
		} `json:"breakdown"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalRequests != 1 || payload.DurationMs.SampleCount != 1 {
		t.Fatalf("analytics=%s", got.Body.String())
	}
	if len(payload.Breakdown) != 1 || payload.Breakdown[0].Provider != "openai-apikey" || payload.Breakdown[0].Requests != 1 {
		t.Fatalf("breakdown=%s", got.Body.String())
	}
}

func TestRoutingProfilesAPIPutAndDelete(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/routing-profiles", strings.NewReader(`{"mode":"create","id":"codex","profile":{"candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}]}}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || !strings.Contains(putRR.Body.String(), `"success":true`) {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK || !strings.Contains(getRR.Body.String(), `"id":"codex"`) || !strings.Contains(getRR.Body.String(), `"policy/codex"`) {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	putIcon := httptest.NewRequest(http.MethodPut, "/api/routing-profiles", strings.NewReader(`{"mode":"update","id":"codex","profile":{"icon":"star","candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}]}}`))
	putIcon.Host = "127.0.0.1"
	putIcon.Header.Set("Content-Type", "application/json")
	putIconRR := httptest.NewRecorder()
	h.ServeHTTP(putIconRR, putIcon)
	if putIconRR.Code != http.StatusOK || !strings.Contains(putIconRR.Body.String(), `"success":true`) {
		t.Fatalf("put icon status=%d body=%s", putIconRR.Code, putIconRR.Body.String())
	}
	getIcon := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	getIcon.Host = "127.0.0.1"
	getIconRR := httptest.NewRecorder()
	h.ServeHTTP(getIconRR, getIcon)
	if getIconRR.Code != http.StatusOK || !strings.Contains(getIconRR.Body.String(), `"icon":"star"`) {
		t.Fatalf("get icon status=%d body=%s", getIconRR.Code, getIconRR.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/routing-profiles?id=codex", nil)
	del.Host = "127.0.0.1"
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK || !strings.Contains(delRR.Body.String(), `"success":true`) {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}
}

func TestRoutingProfilesAPIPreservesExplicitZeroMinQuotaHeadroom(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	createBody := `{"mode":"create","id":"zero-quota","profile":{"alias":"Zero Quota","candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}],"require":{"minQuotaHeadroom":0,"tools":false}}}`
	put := httptest.NewRequest(http.MethodPut, "/api/routing-profiles", strings.NewReader(createBody))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", putRR.Code, putRR.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	var listed struct {
		Profiles []struct {
			ID      string `json:"id"`
			Require struct {
				MinQuotaHeadroom *float64 `json:"minQuotaHeadroom"`
			} `json:"require"`
			Revision string `json:"revision"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	var zero *struct {
		ID      string `json:"id"`
		Require struct {
			MinQuotaHeadroom *float64 `json:"minQuotaHeadroom"`
		} `json:"require"`
		Revision string `json:"revision"`
	}
	for i := range listed.Profiles {
		if listed.Profiles[i].ID == "zero-quota" {
			zero = &listed.Profiles[i]
			break
		}
	}
	if zero == nil {
		t.Fatalf("missing profile in %s", getRR.Body.String())
	}
	if zero.Require.MinQuotaHeadroom == nil || *zero.Require.MinQuotaHeadroom != 0 {
		t.Fatalf("create GET minQuotaHeadroom=%v body=%s", zero.Require.MinQuotaHeadroom, getRR.Body.String())
	}

	updateBody := `{"mode":"update","id":"zero-quota","expectedRevision":"` + zero.Revision + `","profile":{"alias":"Zero Quota","candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}],"require":{"minQuotaHeadroom":0,"tools":true}}}`
	upd := httptest.NewRequest(http.MethodPut, "/api/routing-profiles", strings.NewReader(updateBody))
	upd.Host = "127.0.0.1"
	upd.Header.Set("Content-Type", "application/json")
	updRR := httptest.NewRecorder()
	h.ServeHTTP(updRR, upd)
	if updRR.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updRR.Code, updRR.Body.String())
	}

	get2 := httptest.NewRequest(http.MethodGet, "/api/routing-profiles", nil)
	get2.Host = "127.0.0.1"
	get2RR := httptest.NewRecorder()
	h.ServeHTTP(get2RR, get2)
	if get2RR.Code != http.StatusOK {
		t.Fatalf("get2 status=%d body=%s", get2RR.Code, get2RR.Body.String())
	}
	if err := json.Unmarshal(get2RR.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	zero = nil
	for i := range listed.Profiles {
		if listed.Profiles[i].ID == "zero-quota" {
			zero = &listed.Profiles[i]
			break
		}
	}
	if zero == nil || zero.Require.MinQuotaHeadroom == nil || *zero.Require.MinQuotaHeadroom != 0 {
		t.Fatalf("update GET minQuotaHeadroom missing/nonzero body=%s", get2RR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"minQuotaHeadroom":0`) && !strings.Contains(string(raw), `"minQuotaHeadroom": 0`) {
		t.Fatalf("config lost explicit zero: %s", raw)
	}
}
