package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/requestpolicy"
)

func TestSettingsPUTPersistsAndPublishesRequestPolicy(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"providers\":{}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &handler{configPath: path}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("{\"requestPolicy\":{\"serviceTier\":\"priority\"}}"))
	w := httptest.NewRecorder()
	if !h.serveSettingsPUT(w, req) {
		t.Fatal("settings route not handled")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := requestpolicy.Current().ServiceTier; got != "priority" {
		t.Fatalf("live tier=%q", got)
	}
	var root map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var stored requestpolicy.Policy
	if err := json.Unmarshal(root["requestPolicy"], &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ServiceTier != "priority" {
		t.Fatalf("stored=%#v", stored)
	}
}

func TestSettingsPUTClearsRequestPolicy(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"providers\":{},\"requestPolicy\":{\"serviceTier\":\"priority\"}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = requestpolicy.Publish(requestpolicy.Policy{ServiceTier: "priority"})
	h := &handler{configPath: path}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("{\"requestPolicy\":{}}"))
	w := httptest.NewRecorder()
	h.serveSettingsPUT(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !requestpolicy.Current().Empty() {
		t.Fatalf("live policy=%#v", requestpolicy.Current())
	}
	var root map[string]json.RawMessage
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &root)
	if _, exists := root["requestPolicy"]; exists {
		t.Fatalf("requestPolicy remained in config: %s", data)
	}
}

func TestSettingsPUTRejectsInvalidRequestPolicyWithoutPublishingIt(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"providers\":{}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = requestpolicy.Publish(requestpolicy.Policy{ServiceTier: "flex"})
	h := &handler{configPath: path}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("{\"requestPolicy\":{\"serviceTier\":\"fast\"}}"))
	w := httptest.NewRecorder()
	h.serveSettingsPUT(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := requestpolicy.Current().ServiceTier; got != "flex" {
		t.Fatalf("invalid write changed live tier to %q", got)
	}
}
