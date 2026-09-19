package server

import (
	"net/http"
	"testing"
)

func TestSnapshotForwardHeadersPreservesCallerUserAgent(t *testing.T) {
	header := make(http.Header)
	header.Set("User-Agent", "codex-cli/9.9 compatibility-marker")
	header.Set("Cookie", "must-not-pass")

	snapshot := (&handler{}).snapshotForwardHeaders(header)
	if got := snapshot.Get("user-agent"); got != "codex-cli/9.9 compatibility-marker" {
		t.Fatalf("user-agent=%q", got)
	}
	if got := snapshot.Get("cookie"); got != "" {
		t.Fatalf("cookie leaked=%q", got)
	}
}
