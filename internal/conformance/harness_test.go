package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

// Provenance identifies a fixture so it cannot silently mutate at runtime.
// Source is a versioned name; CapturedAt is the authored timestamp, not now().
type Provenance struct {
	Source     string
	CapturedAt string
	Reviewer   string
}

func (p Provenance) Validate() error {
	if strings.TrimSpace(p.Source) == "" {
		return fmt.Errorf("fixture provenance source is required")
	}
	if !strings.Contains(p.Source, "@") {
		return fmt.Errorf("fixture provenance source %q must include a version", p.Source)
	}
	if strings.TrimSpace(p.CapturedAt) == "" {
		return fmt.Errorf("fixture provenance capturedAt is required")
	}
	if strings.TrimSpace(p.Reviewer) == "" {
		return fmt.Errorf("fixture provenance reviewer is required")
	}
	return nil
}

type capturedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

type captureTransport struct {
	mu      sync.Mutex
	first   capturedRequest
	status  int
	ctype   string
	payload []byte
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	c.mu.Lock()
	if c.first.Method == "" {
		c.first = capturedRequest{
			Method: req.Method,
			URL:    req.URL.String(),
			Header: req.Header.Clone(),
			Body:   append([]byte(nil), body...),
		}
	}
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	ctype := c.ctype
	payload := c.payload
	c.mu.Unlock()
	header := make(http.Header)
	header.Set("Content-Type", ctype)
	return &http.Response{
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
		Request:       req,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
	}, nil
}

func (c *captureTransport) captured() capturedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.first
}

func newCapture(ctype string, payload []byte) (*captureTransport, *http.Client) {
	trip := &captureTransport{status: http.StatusOK, ctype: ctype, payload: payload}
	var rt http.RoundTripper = trip
	return trip, &http.Client{Transport: rt}
}

func collectEvents(stream providers.EventStream) ([]protocol.Event, error) {
	if stream == nil {
		return nil, fmt.Errorf("stream is required")
	}
	defer stream.Close()
	var out []protocol.Event
	for {
		ev, err := stream.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if err := ev.Validate(); err != nil {
			return out, err
		}
		out = append(out, ev)
	}
}

func sseBytes(payloads ...string) []byte {
	var b strings.Builder
	for _, payload := range payloads {
		b.WriteString("data: ")
		b.WriteString(payload)
		b.WriteString("\n\n")
	}
	return []byte(b.String())
}

func eventTypes(events []protocol.Event) []protocol.EventType {
	out := make([]protocol.EventType, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}

func requireEventTypes(t *testing.T, events []protocol.Event, want ...protocol.EventType) {
	t.Helper()
	got := eventTypes(events)
	if len(got) != len(want) {
		t.Fatalf("event types=%v want=%v events=%#v", got, want, events)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d]=%s want=%s events=%#v", i, got[i], want[i], events)
		}
	}
}

func requireJSONSubset(t *testing.T, raw []byte, want map[string]any) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("captured request body is empty")
	}
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("captured body is not JSON: %v\n%s", err, raw)
	}
	if err := jsonSubset(got, want); err != nil {
		t.Fatalf("captured body subset: %v\n%s", err, raw)
	}
}

func jsonSubset(got, want any) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("want object, got %T", got)
		}
		for key, child := range w {
			actual, ok := g[key]
			if !ok {
				return fmt.Errorf("missing %q", key)
			}
			if err := jsonSubset(actual, child); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		return nil
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Errorf("want array, got %T", got)
		}
		if len(g) < len(w) {
			return fmt.Errorf("array length %d < %d", len(g), len(w))
		}
		for i := range w {
			if err := jsonSubset(g[i], w[i]); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	default:
		if !reflect.DeepEqual(normalizeJSON(got), normalizeJSON(want)) {
			return fmt.Errorf("got %#v want %#v", got, want)
		}
		return nil
	}
}

func normalizeJSON(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return n.String()
		}
		return f
	default:
		return v
	}
}

func concatText(events []protocol.Event) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.Type == protocol.EventTextDelta {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

func firstToolName(events []protocol.Event) string {
	for _, ev := range events {
		if ev.Type == protocol.EventToolCallStart || ev.Type == protocol.EventToolCallEnd {
			if ev.Name != "" {
				return ev.Name
			}
		}
	}
	return ""
}

func mustProvenance(t *testing.T, p Provenance) {
	t.Helper()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}
