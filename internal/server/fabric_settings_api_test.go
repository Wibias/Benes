package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFabricSettingsAPIReadsAndWritesEnabled(t *testing.T) {
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
	get := httptest.NewRequest(http.MethodGet, "/api/fabric-settings", nil)
	get.Host = "127.0.0.1"
	got := httptest.NewRecorder()
	h.ServeHTTP(got, get)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"enabled":false`) {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	put := httptest.NewRequest(http.MethodPut, "/api/fabric-settings", strings.NewReader(`{"enabled":true}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	status := httptest.NewRequest(http.MethodGet, "/api/fabric/status", nil)
	status.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, status)
	if !strings.Contains(statusRR.Body.String(), `"enabled":true`) {
		t.Fatalf("status=%s", statusRR.Body.String())
	}
	tasks := httptest.NewRequest(http.MethodGet, "/api/fabric/tasks", nil)
	tasks.Host = "127.0.0.1"
	tasksRR := httptest.NewRecorder()
	h.ServeHTTP(tasksRR, tasks)
	if tasksRR.Code != http.StatusOK {
		t.Fatalf("tasks status=%d body=%s", tasksRR.Code, tasksRR.Body.String())
	}
}

func TestFabricSettingsAPIRejectsNonLoopback(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
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
	req := httptest.NewRequest(http.MethodGet, "/api/fabric-settings", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-loopback status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFabricSettingsAPIPreservesUnrelatedConfig(t *testing.T) {
	h, home := newFabricTestHandler(t, false)
	put := fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":true}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", put.Code, put.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root["defaultProvider"] != "openai-apikey" {
		t.Fatalf("unrelated field lost: %s", raw)
	}
	providers, _ := root["providers"].(map[string]any)
	if providers["openai-apikey"] == nil {
		t.Fatalf("providers lost: %s", raw)
	}
}

func TestFabricSettingsAPIRejectsMalformed(t *testing.T) {
	h, _ := newFabricTestHandler(t, false)
	for _, body := range []string{`{`, `{"enabled":true}{"enabled":false}`, `{}`, `{"enabled":true,"worker":1}`, `{"enabled":"yes"}`} {
		rr := fabricDo(h, http.MethodPut, "/api/fabric-settings", body)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"code":"invalid_body"`) {
			t.Fatalf("body=%s status=%d resp=%s", body, rr.Code, rr.Body.String())
		}
	}
	disable := fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":false}`)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	status := fabricDo(h, http.MethodGet, "/api/fabric/status", "")
	if !strings.Contains(status.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%s", status.Body.String())
	}
}

func TestFabricSettingsAPIConcurrentUpdates(t *testing.T) {
	h, home := newFabricTestHandler(t, false)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":true}`)
	}()
	go func() {
		defer wg.Done()
		_ = fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":false}`)
	}()
	wg.Wait()
	raw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Enabled         any    `json:"-"`
		DefaultProvider string `json:"defaultProvider"`
		Fabric          struct {
			Enabled bool `json:"enabled"`
		} `json:"fabric"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root.DefaultProvider != "openai-apikey" {
		t.Fatalf("concurrent put lost unrelated config: %s", raw)
	}
	status := fabricDo(h, http.MethodGet, "/api/fabric/status", "")
	if !strings.Contains(status.Body.String(), `"enabled":true`) && !strings.Contains(status.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%s", status.Body.String())
	}
}

func TestFabricDisableDoesNotMutateTasks(t *testing.T) {
	h, home := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"keep"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	start := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"operator"}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	if disable := fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":false}`); disable.Code != http.StatusOK {
		t.Fatalf("disable=%s", disable.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "fabric", created.ID+".events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "TaskCompleted") || strings.Contains(string(raw), "RunCompleted") {
		t.Fatalf("disable emitted close: %s", raw)
	}
	if enable := fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":true}`); enable.Code != http.StatusOK {
		t.Fatalf("enable=%s", enable.Body.String())
	}
	get := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"runState":"running"`) {
		t.Fatalf("re-enable get=%d %s", get.Code, get.Body.String())
	}
}
