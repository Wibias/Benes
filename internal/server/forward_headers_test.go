package server

import (
	"net/http"
	"testing"
)

func TestSnapshotForwardHeadersUsesExactAllowlistAndCopies(t *testing.T) {
	header := make(http.Header)
	approved := map[string]string{
		"Authorization":                         "auth-marker",
		"ChatGPT-Account-ID":                    "acct-public",
		"OpenAI-Beta":                           "beta-marker",
		"Originator":                            "codex-cli",
		"Session_ID":                            "session-underscore",
		"Session-ID":                            "session-dash",
		"Thread-ID":                             "thread-1",
		"X-Client-Request-ID":                   "request-1",
		"X-Codex-Beta-Features":                 "feature-1",
		"X-Codex-Installation-ID":               "install-1",
		"X-Codex-Parent-Thread-ID":              "parent-1",
		"X-Codex-Turn-Metadata":                 "metadata-1",
		"X-Codex-Turn-State":                    "state-1",
		"X-Codex-Window-ID":                     "window-1",
		"X-OAI-Attestation":                     "attestation-1",
		"X-OpenAI-Subagent":                     "subagent-1",
		"X-ResponsesAPI-Include-Timing-Metrics": "true",
	}
	for name, value := range approved {
		header.Set(name, value)
	}
	header.Set("X-Benes-API-Key", "local-marker")
	header.Set("Cookie", "cookie-marker")
	header.Set("X-API-Key", "generic-marker")
	header.Set("X-Caller-Secret", "unknown-marker")

	h := &handler{}
	snapshot := h.snapshotForwardHeaders(header)
	for name, want := range approved {
		if got := snapshot.Get(name); got != want {
			t.Fatalf("%s=%q want %q", name, got, want)
		}
	}
	for _, forbidden := range []string{"X-Benes-API-Key", "Cookie", "X-API-Key", "X-Caller-Secret"} {
		if got := snapshot.Get(forbidden); got != "" {
			t.Fatalf("forbidden %s leaked as %q", forbidden, got)
		}
	}

	header.Set("Authorization", "changed-marker")
	if got := snapshot.Get("authorization"); got != "auth-marker" {
		t.Fatalf("snapshot mutated: %q", got)
	}
}

func TestSnapshotForwardHeadersBlocksLocalAdmissionBearerOnly(t *testing.T) {
	hashes, err := hashAdmissionTokens([]string{"local-secret"})
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{admission: dataPlaneAdmission{tokenHashes: hashes}}

	local := make(http.Header)
	local.Set("Authorization", "Bearer local-secret")
	local.Set("X-Codex-Window-ID", "window-1")
	blocked := h.snapshotForwardHeaders(local)
	if !blocked.BlockedAuthorization() {
		t.Fatal("local admission bearer was not marked blocked")
	}
	if got := blocked.Get("authorization"); got != "" {
		t.Fatalf("local admission bearer retained: %q", got)
	}
	if got := blocked.Get("x-codex-window-id"); got != "window-1" {
		t.Fatalf("window id=%q", got)
	}

	caller := make(http.Header)
	caller.Set("Authorization", "Bearer caller-oauth-marker")
	allowed := h.snapshotForwardHeaders(caller)
	if allowed.BlockedAuthorization() {
		t.Fatal("distinct caller authorization was blocked")
	}
	if got := allowed.Get("authorization"); got != "Bearer caller-oauth-marker" {
		t.Fatalf("caller authorization=%q", got)
	}
}
