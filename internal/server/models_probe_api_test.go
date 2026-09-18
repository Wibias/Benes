package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/modelprobe"
)

func TestModelProbeAPICatalogAndCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-x"}]}`))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := `{"providers":{"lab":{"adapter":"openai-chat","baseUrl":"` + upstream.URL + `/v1","apiKey":"sk-secret","allowPrivateNetwork":true}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"lab": providerFunc(nil)}, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blocked := httptest.NewRequest(http.MethodPost, "/api/models/probe", strings.NewReader(`{"provider":"lab","model":"gpt-x"}`))
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("non-loopback status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/models/probe?provider=lab&model=gpt-x", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	var cached modelprobe.Result
	if err := json.Unmarshal(getRR.Body.Bytes(), &cached); err != nil {
		t.Fatal(err)
	}
	if cached.State != modelprobe.StateUnknown || cached.Reason != "unprobed" {
		t.Fatalf("unprobed=%s", getRR.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/api/models/probe", strings.NewReader(`{"provider":"lab","model":"gpt-x"}`))
	post.Host = "127.0.0.1"
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusOK {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	if strings.Contains(postRR.Body.String(), "sk-secret") {
		t.Fatal("secret leaked")
	}
	var result modelprobe.Result
	if err := json.Unmarshal(postRR.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != modelprobe.StateAvailable || result.GenerationIncurred {
		t.Fatalf("%#v", result)
	}

	get2 := httptest.NewRequest(http.MethodGet, "/api/models/probe?provider=lab&model=gpt-x", nil)
	get2.Host = "127.0.0.1"
	get2RR := httptest.NewRecorder()
	h.ServeHTTP(get2RR, get2)
	var remembered modelprobe.Result
	if err := json.Unmarshal(get2RR.Body.Bytes(), &remembered); err != nil {
		t.Fatal(err)
	}
	if remembered.State != modelprobe.StateAvailable {
		t.Fatalf("cache=%s", get2RR.Body.String())
	}
}

func TestModelProbeAPIDoesNotGenerateUnlessRequested(t *testing.T) {
	methods := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"lab":{"adapter":"openai-chat","baseUrl":"`+upstream.URL+`","apiKey":"k","allowPrivateNetwork":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"lab": providerFunc(nil)}, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	post := httptest.NewRequest(http.MethodPost, "/api/models/probe", strings.NewReader(`{"provider":"lab","model":"m"}`))
	post.Host = "127.0.0.1"
	h.ServeHTTP(httptest.NewRecorder(), post)
	for _, method := range methods {
		if method == http.MethodPost {
			t.Fatal("paid generation without generate=true")
		}
	}
}
