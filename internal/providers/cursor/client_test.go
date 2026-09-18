package cursor

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestNewHardenedRejectsCleartextAndMissingToken(t *testing.T) {
	if _, err := NewHardened(context.Background(), Config{Endpoint: "http://api2.cursor.sh", APIKey: "tok", HTTPVersion: HTTPVersion1Dot1}); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("cleartext=%v", err)
	}
	if _, err := NewHardened(context.Background(), Config{Endpoint: DefaultAPI, HTTPClient: unusedClient()}); err == nil {
		t.Fatal("missing token")
	}
}

func TestOpenRejectsRemoteVisionBeforeDispatch(t *testing.T) {
	client, err := NewHardened(context.Background(), Config{Endpoint: DefaultAPI, APIKey: "tok", HTTPClient: unusedClient()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "https://example/x.png"}},
		}}},
	}})
	if err == nil {
		t.Fatal("remote vision")
	}
}

type rewriteHost struct {
	base string
	next http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(r.base)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.Host = target.Host
	return r.next.RoundTrip(req)
}

func unusedClient() *http.Client {
	return &http.Client{}
}

func TestOpenPostsBearerAfterHTTPSAndReadsConnectFrame(t *testing.T) {
	var gotAuth, gotPath string
	frame, err := EncodeConnectFrame([]byte("hello"), false)
	if err != nil {
		t.Fatal(err)
	}
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(frame, end...))
	})
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gpt-5.4"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil || ev.Text != "hello" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	if gotAuth != "Bearer tok" || gotPath != runPath {
		t.Fatalf("auth=%q path=%q", gotAuth, gotPath)
	}
}

func TestOpenCheckpointHitKeepsToolResultSuffix(t *testing.T) {
	parsed := protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
				Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup",
			}}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found-suffix"}}},
		}},
	}
	compiled, err := CompileRun(parsed)
	if err != nil {
		t.Fatal(err)
	}
	var saw []byte
	frame, err := EncodeConnectFrame([]byte("hello"), false)
	if err != nil {
		t.Fatal(err)
	}
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		n, _ := r.Body.Read(buf)
		saw = append([]byte(nil), buf[:n]...)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(frame, end...))
	})
	client.checkpoints.Remember(Checkpoint{
		ConversationID: "local",
		Identity:       "default",
		Model:          compiled.Model,
		PrefixDigest:   compiled.Digest,
		Bytes:          []byte("ckpt-blob"),
		StoredAt:       time.Now(),
	})
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: parsed})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saw, BlobID([]byte("ckpt-blob"))) {
		t.Fatalf("checkpoint missing from Open payload")
	}
	frames, _, err := DecodeConnectFrames(saw)
	if err != nil || len(frames) == 0 {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	run := mustFields(t, fieldBytes(mustFields(t, frames[0].Payload), 1))
	action := mustFields(t, fieldBytes(run, 2))
	if _, ok := fieldBytesPresent(action, 2); !ok {
		t.Fatal("checkpoint hit dropped the tool-result suffix (expected resume_action)")
	}
}
