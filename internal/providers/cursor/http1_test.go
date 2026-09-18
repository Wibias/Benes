package cursor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestHTTP1WaitsForRequestIDBeforeAppend(t *testing.T) {
	var paths []string
	frame, err := EncodeConnectFrame([]byte("hello"), false)
	if err != nil {
		t.Fatal(err)
	}
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == runSSEPath {
			w.Header().Set("X-Request-Id", "req-1")
			w.Write(append(frame, end...))
			return
		}
		if r.Header.Get("X-Request-Id") != "req-1" || r.Header.Get("X-Cursor-Seq") != "1" {
			http.Error(w, "bad append", http.StatusBadRequest)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:    DefaultAPI,
		APIKey:      "tok",
		HTTPVersion: HTTPVersion1Dot1,
		HTTPClient:  &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gpt-5.4"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(paths) < 2 || paths[0] != runSSEPath || paths[1] != appendPath {
		t.Fatalf("paths=%v", paths)
	}
}

func TestHTTP1WritesGetBlobReplyOnAppend(t *testing.T) {
	compiled, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{
			SystemPrompt: []string{"sys"},
			Messages:     []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	roots := RootPromptBlobs(compiled)
	args := EncodeProtoMessage(4, append(EncodeProtoVarint(1, 3), EncodeProtoMessage(2, EncodeProtoBytes(1, BlobID(roots[0])))...))
	ask, err := EncodeConnectFrame(args, false)
	if err != nil {
		t.Fatal(err)
	}
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == runSSEPath {
			w.Header().Set("X-Request-Id", "req-kv")
			w.Write(append(ask, end...))
			return
		}
		seqs = append(seqs, r.Header.Get("X-Cursor-Seq"))
		io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:    DefaultAPI,
		APIKey:      "tok",
		HTTPVersion: HTTPVersion1Dot1,
		HTTPClient:  &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{
			SystemPrompt: []string{"sys"},
			Messages:     []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_, _ = stream.Next()
	if len(seqs) < 2 || seqs[1] != "2" {
		t.Fatalf("seqs=%v", seqs)
	}
}
