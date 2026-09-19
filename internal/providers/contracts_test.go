package providers

import "testing"

func TestForwardHeadersDefensivelyCopyFilterAndLookupCaseInsensitively(t *testing.T) {
	source := map[string]string{
		"Authorization":     "auth-marker",
		"X-Codex-Window-ID": "window-1",
		"Cookie":            "cookie-marker",
		"X-Caller-Secret":   "unknown-marker",
		"X-Benes-Surface":   "codex",
	}
	snapshot := NewForwardHeaders(source)
	source["Authorization"] = "changed-marker"

	if got := snapshot.Get("authorization"); got != "auth-marker" {
		t.Fatalf("authorization=%q", got)
	}
	if got := snapshot.Get("X-CODEX-WINDOW-ID"); got != "window-1" {
		t.Fatalf("window id=%q", got)
	}
	for _, forbidden := range []string{"cookie", "x-caller-secret", "missing", "x-benes-surface"} {
		if got := snapshot.Get(forbidden); got != "" {
			t.Fatalf("%s=%q", forbidden, got)
		}
	}

	values := snapshot.Values()
	values["authorization"] = "mutated-export"
	if got := snapshot.Get("authorization"); got != "auth-marker" {
		t.Fatalf("export mutation changed snapshot: %q", got)
	}
}

func TestForwardHeadersBlockProxyAdmissionAuthorization(t *testing.T) {
	snapshot := NewForwardHeadersWithBlockedAuthorization(map[string]string{
		"authorization":     "proxy-admission-marker",
		"x-codex-window-id": "window-1",
	}, true)
	if !snapshot.BlockedAuthorization() {
		t.Fatal("blocked authorization marker was not retained")
	}
	if got := snapshot.Get("authorization"); got != "" {
		t.Fatalf("blocked authorization retained: %q", got)
	}
	if got := snapshot.Get("x-codex-window-id"); got != "window-1" {
		t.Fatalf("window id=%q", got)
	}
}

func TestForwardHeaderNamesReturnsCopy(t *testing.T) {
	names := ForwardHeaderNames()
	if len(names) != 17 {
		t.Fatalf("names=%v", names)
	}
	names[0] = "mutated"
	if got := ForwardHeaderNames()[0]; got != "authorization" {
		t.Fatalf("allowlist mutated: %q", got)
	}
}

func TestForwardHeadersUserAgentIsBoundedAndSingleLine(t *testing.T) {
	valid := NewForwardHeaders(map[string]string{"User-Agent": "codex-cli/9.9 compat"})
	if got := valid.Get("user-agent"); got != "codex-cli/9.9 compat" {
		t.Fatalf("valid user-agent=%q", got)
	}
	for _, value := range []string{
		" leading",
		"trailing ",
		"line\nbreak",
		"carriage\rreturn",
		strings.Repeat("x", MaxUserAgentBytes+1),
	} {
		if got := NewForwardHeaders(map[string]string{"User-Agent": value}).Get("user-agent"); got != "" {
			t.Fatalf("unsafe user-agent %q survived as %q", value, got)
		}
	}
}

func TestApplyUserAgentConfiguredValueWinsCallerFallback(t *testing.T) {
	header := make(http.Header)
	ApplyUserAgent(header, "provider-agent/1", NewForwardHeaders(map[string]string{"User-Agent": "caller-agent/2"}))
	if got := header.Get("User-Agent"); got != "provider-agent/1" {
		t.Fatalf("user-agent=%q", got)
	}
}

