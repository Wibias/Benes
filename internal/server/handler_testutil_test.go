package server

import (
	"net/http"
	"testing"
)

func testHandler(t *testing.T, opts Options) *handler {
	t.Helper()
	raw, err := NewHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := raw.(*handler)
	if !ok {
		t.Fatalf("NewHandler returned %T, want *handler", raw)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// attachHandlerClose registers Indexer/data-plane Close on test cleanup so
// Windows can unlink routing-history.sqlite after async catch-up workers exit.
func attachHandlerClose(t *testing.T, h http.Handler) http.Handler {
	t.Helper()
	if c, ok := h.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
	return h
}

var _ http.Handler = (*handler)(nil)
