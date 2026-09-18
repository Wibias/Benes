package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/credentials"
)

func TestProviderKeyMutationsEmitSemanticActivityEvents(t *testing.T) {
	store, err := credentials.NewFileStore(filepath.Join(t.TempDir(), "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := newProvidersMutateHandlerWithCreds(t, `{
		"providers": {
			"custom": {"adapter":"openai-chat","baseUrl":"https://example.com/v1","authMode":"key"}
		}
	}`, store, "custom")
	inner := h.(*handler)

	add := loopbackJSON(http.MethodPost, "/api/providers/keys", `{"name":"custom","key":"sk-test-secret","label":"Primary"}`)
	addRR := httptest.NewRecorder()
	h.ServeHTTP(addRR, add)
	if addRR.Code != http.StatusCreated {
		t.Fatalf("add status=%d body=%s", addRR.Code, addRR.Body.String())
	}
	var added struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(addRR.Body.Bytes(), &added); err != nil || added.ID == "" {
		t.Fatalf("add body=%s err=%v", addRR.Body.String(), err)
	}
	assertProviderActivityTypes(t, inner, []string{"api_key_added"})

	selectReq := loopbackJSON(http.MethodPut, "/api/providers/keys/active", fmt.Sprintf(`{"name":"custom","id":%q}`, added.ID))
	selectRR := httptest.NewRecorder()
	h.ServeHTTP(selectRR, selectReq)
	if selectRR.Code != http.StatusOK {
		t.Fatalf("select status=%d body=%s", selectRR.Code, selectRR.Body.String())
	}
	assertProviderActivityTypes(t, inner, []string{"api_key_added", "api_key_selected"})

	remove := httptest.NewRequest(http.MethodDelete, "/api/providers/keys?name=custom&id="+added.ID, nil)
	remove.Host = "127.0.0.1"
	removeRR := httptest.NewRecorder()
	h.ServeHTTP(removeRR, remove)
	if removeRR.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", removeRR.Code, removeRR.Body.String())
	}
	assertProviderActivityTypes(t, inner, []string{"api_key_added", "api_key_selected", "api_key_removed"})

	for _, event := range inner.activity.Recent(20) {
		if event.Type == "api_key_used" {
			t.Fatalf("credential mutation emitted api_key_used: %#v", event)
		}
		if event.Detail != "" {
			t.Fatalf("credential identity leaked into activity detail: %#v", event)
		}
	}
}

func assertProviderActivityTypes(t *testing.T, h *handler, want []string) {
	t.Helper()
	events := h.activity.Recent(20)
	got := make([]string, 0, len(events))
	for _, event := range events {
		if event.Provider == "custom" {
			got = append(got, event.Type)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("activity types=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("activity types=%v want=%v", got, want)
		}
	}
}
