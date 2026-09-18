package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestResponsesProviderResolveExactlyOnceAndSharedAuthority(t *testing.T) {
	p := &fabricExecProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hi"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	before := providerResolveCallCount.Load()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5","input":"x","stream":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	after := providerResolveCallCount.Load()
	if after-before != 1 {
		t.Fatalf("responses resolve count=%d want 1; status=%d body=%s", after-before, w.Code, w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if p.opens.Load() != 1 {
		t.Fatalf("opens=%d", p.opens.Load())
	}
}

func TestFabricProviderResolveExactlyOnce(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	before := providerResolveCallCount.Load()
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	waitFabricRunState(t, h, id, "completed")
	after := providerResolveCallCount.Load()
	if after-before != 1 {
		t.Fatalf("fabric resolve count=%d want 1", after-before)
	}
}

func TestFabricComboNoteOpenAfterOpenCapturesAttempts(t *testing.T) {
	first := &fabricExecProvider{fail: true}
	second := &fabricExecProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "b-ok"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 2, OutputTokens: 3}},
	}}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		ConfigPath:     configPath,
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "google", Model: "gemini", Protocol: "google"},
				{ProviderID: "openai-apikey", Model: "gpt-5", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"combo/fast","input":"x"}`)
	waitFabricRunState(t, h, id, "completed")
	if first.opens.Load() != 1 || second.opens.Load() != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens.Load(), second.opens.Load())
	}

	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	if len(listed.Requests) == 0 {
		t.Fatal("no diagnostics rows")
	}
	detail := getDiagnosticsDetail(t, h, listed.Requests[0].RequestID)
	if len(detail.Attempts) < 2 || detail.Attempts[0].Decision != "hop" || detail.Attempts[len(detail.Attempts)-1].Decision != "committed" {
		t.Fatalf("attempts after Open=%s", mustJSON(detail.Attempts))
	}
	if detail.Routing == nil || detail.Routing.CommittedMember == "" && detail.Routing.Provider != "openai-apikey" {
		// Prefer committed member when present; provider attribution still names B.
		if detail.Routing == nil || (detail.Routing.Provider != "openai-apikey" && !strings.Contains(detail.Attempts[len(detail.Attempts)-1].Member, "openai-apikey")) {
			t.Fatalf("committed B missing: routing=%s attempts=%s", mustJSON(detail.Routing), mustJSON(detail.Attempts))
		}
	}
	usagePath := filepath.Join(home, "usage", "active.jsonl")
	raw, err := os.ReadFile(usagePath)
	if err != nil {
		raw, err = os.ReadFile(filepath.Join(home, "usage.jsonl"))
		if err != nil {
			t.Fatalf("usage missing: %v", err)
		}
	}
	if !strings.Contains(string(raw), "openai-apikey") && !strings.Contains(string(raw), "gpt-5") {
		t.Fatalf("usage missing committed B: %s", raw)
	}
}

func TestFabricComboExhaustedCapturesAttemptsNoFakeCommit(t *testing.T) {
	a := &fabricExecProvider{fail: true}
	b := &fabricExecProvider{fail: true}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": a, "openai-apikey": b},
		ConfigPath:     configPath,
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "google", Model: "gemini", Protocol: "google"},
				{ProviderID: "openai-apikey", Model: "gpt-5", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"combo/fast","input":"x"}`)
	waitFabricRunState(t, h, id, "failed")
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	if len(listed.Requests) == 0 {
		t.Fatal("no diagnostics rows")
	}
	detail := getDiagnosticsDetail(t, h, listed.Requests[0].RequestID)
	if len(detail.Attempts) < 2 {
		t.Fatalf("exhausted combo should capture attempts after failed Open: %s", mustJSON(detail.Attempts))
	}
	for _, a := range detail.Attempts {
		if a.Decision == "committed" {
			t.Fatalf("exhausted combo must not invent committed: %s", mustJSON(detail.Attempts))
		}
	}
	if detail.Routing != nil && detail.Routing.CommittedMember != "" {
		t.Fatalf("fake committed member %q", detail.Routing.CommittedMember)
	}
}

func TestClampFabricResultOutputPreservesWhitespaceAndUTF8(t *testing.T) {
	got, trunc := clampFabricResultOutput("  hello  ")
	if trunc || got != "  hello  " {
		t.Fatalf("whitespace mutated: %q trunc=%v", got, trunc)
	}
	below := strings.Repeat("a", fabricResultMaxPerItem)
	got, trunc = clampFabricResultOutput(below)
	if trunc || len(got) != fabricResultMaxPerItem {
		t.Fatalf("exact cap trunc=%v len=%d", trunc, len(got))
	}
	prefix := strings.Repeat("b", fabricResultMaxPerItem-1)
	raw := prefix + "⌘extra"
	got, trunc = clampFabricResultOutput(raw)
	if !trunc {
		t.Fatal("expected truncated")
	}
	if len(got) > fabricResultMaxPerItem {
		t.Fatalf("len=%d > cap", len(got))
	}
	if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("invalid utf8 after truncate: %q", got)
	}

	s := newFabricResultStore()
	handle := s.put(fabricStoredResult{
		Handle: "fr_ws", RunID: "r", TaskID: "t", Status: "completed",
		Output: "  padded  ", RequestID: "req_x",
	})
	item, ok := s.get(handle)
	if !ok || item.Output != "  padded  " || item.Truncated {
		t.Fatalf("store mutated whitespace: %#v", item)
	}

	// End-to-end Fabric result preserves leading/trailing spaces.
	h, _ := newFabricExecHandler(t, &fabricExecProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "  spaced  "},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	runID, _ := body["runId"].(string)
	waitFabricRunState(t, h, id, "completed")
	res := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id+"/runs/"+runID+"/result", "")
	var gotRes map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &gotRes)
	if gotRes["output"] != "  spaced  " {
		t.Fatalf("result output mutated: %#v", gotRes["output"])
	}
	if gotRes["truncated"] == true {
		t.Fatal("below-cap output must not set truncated")
	}
}

func TestFabricCollectSealsAfterUTF8OmitNoLaterFill(t *testing.T) {
	// Stream: Repeat("A", cap-1) then "€" then "X".
	// After omitting the multibyte rune at the UTF-8-safe cap, a later ASCII
	// delta must not refill leftover bytes (exact-prefix of model-visible text).
	prefix := strings.Repeat("A", fabricResultMaxPerItem-1)
	h, _ := newFabricExecHandler(t, &fabricExecProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: prefix},
		{Type: protocol.EventTextDelta, Text: "€"},
		{Type: protocol.EventTextDelta, Text: "X"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	runID, _ := body["runId"].(string)
	waitFabricRunState(t, h, id, "completed")
	res := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id+"/runs/"+runID+"/result", "")
	var gotRes map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &gotRes)
	out, _ := gotRes["output"].(string)
	if out != prefix {
		t.Fatalf("output must be exact A prefix; got len=%d containsEuro=%v containsX=%v", len(out), strings.Contains(out, "€"), strings.Contains(out, "X"))
	}
	if strings.Contains(out, "€") || strings.Contains(out, "X") {
		t.Fatalf("sealed output leaked omitted/later text: %q", out)
	}
	if !utf8.ValidString(out) {
		t.Fatal("output must be valid UTF-8")
	}
	if len(out) > fabricResultMaxPerItem {
		t.Fatalf("len=%d > cap", len(out))
	}
	if gotRes["truncated"] != true {
		t.Fatalf("expected truncated=true, got %#v", gotRes["truncated"])
	}
}

func TestPreResolvedSkipsSecondResolveInsideModelTurn(t *testing.T) {
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fabricExecProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := attachHandlerClose(t, raw).(*handler)
	route, err := h.parseRoute("openai-apikey/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	before := providerResolveCallCount.Load()
	resolved := h.resolveProviderWithEvidence(route, policyRequestEvidence{})
	mid := providerResolveCallCount.Load()
	if mid-before != 1 {
		t.Fatalf("setup resolve=%d", mid-before)
	}
	_, turnErr := h.runModelTurn(httptest.NewRequest(http.MethodPost, "/", nil).Context(), modelTurnInput{
		Model:            "openai-apikey/gpt-5",
		Input:            "x",
		PreResolvedRoute: &route,
		PreResolved:      &resolved,
		Persist:          false,
	})
	if turnErr != nil {
		t.Fatal(turnErr)
	}
	after := providerResolveCallCount.Load()
	if after != mid {
		t.Fatalf("runModelTurn must not resolve again when PreResolved set; delta=%d", after-mid)
	}
}
