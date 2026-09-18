package cursor

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestNormalizeCursorUsageMarksEstimateAndPrefersProvider(t *testing.T) {
	est := NormalizeCursorUsage(99, 0, 4, 20)
	if !est.Estimated || est.InputTokens != 20 || est.TotalTokens != 24 || est.CacheReadInputTokens != 0 {
		t.Fatalf("capped estimate=%#v", est)
	}
	real := NormalizeCursorUsage(3, 40, 5, 100)
	if real.Estimated || real.InputTokens != 40 || real.TotalTokens != 45 {
		t.Fatalf("provider=%#v", real)
	}
}

func TestCheckpointReleasesBlobsAndDropsIsolated(t *testing.T) {
	blobs := NewBlobStore()
	id := blobs.Put([]byte("leased"))
	store := NewCheckpointStore()
	store.maxN = 1
	store.BindBlobs(blobs)
	store.Remember(Checkpoint{ConversationID: "c", Identity: "i", Model: "m", PrefixDigest: "d", Bytes: []byte("one"), BlobIDs: [][]byte{id}, StoredAt: time.Now()})
	store.Remember(Checkpoint{ConversationID: "c2", Identity: "i", Model: "m", PrefixDigest: "e", Bytes: []byte("two"), StoredAt: time.Now()})
	if _, ok := blobs.Get(id); ok {
		t.Fatal("evicted lease must release")
	}
	if _, ok := store.Lookup("c", "i", "m", "d"); ok {
		t.Fatal("evicted")
	}
	store.Remember(Checkpoint{ConversationID: "iso", Identity: "i", Model: "m", PrefixDigest: "x", Bytes: []byte("nope"), Isolated: true, StoredAt: time.Now()})
	if _, ok := store.Lookup("iso", "i", "m", "x"); ok {
		t.Fatal("isolated")
	}
}

func TestCaptureRejectsNoStoreAndHelpers(t *testing.T) {
	off := false
	if checkpointCaptureAllowed(providers.DispatchRequest{Parsed: protocol.ParsedRequest{Options: protocol.RequestOptions{Store: &off}}}) {
		t.Fatal("no-store")
	}
	if checkpointCaptureAllowed(providers.DispatchRequest{ForwardHeaders: providers.NewForwardHeaders(map[string]string{"x-openai-subagent": "1"})}) {
		t.Fatal("subagent")
	}
	if !checkpointCaptureAllowed(providers.DispatchRequest{}) {
		t.Fatal("ordinary")
	}
}

func TestInvalidArgumentForgetsCheckpoint(t *testing.T) {
	if !isInvalidArgument(fmtInvalid()) {
		t.Fatal("detect")
	}
	store := NewCheckpointStore()
	digest := PrefixDigest(nil, []string{"hi"})
	store.Remember(Checkpoint{ConversationID: "local", Identity: "default", Model: "gpt-5.4", PrefixDigest: digest, Bytes: []byte("ckpt"), StoredAt: time.Now()})
	store.Forget("local", "default", "gpt-5.4", digest)
	if _, ok := store.Lookup("local", "default", "gpt-5.4", digest); ok {
		t.Fatal("forgotten")
	}
}

func TestDiscoverUsableModelsUsesPinnedTransport(t *testing.T) {
	var proto int
	var path string
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto = r.ProtoMajor
		path = r.URL.Path
		w.Write(EncodeProtoMessage(1, EncodeProtoString(1, "gpt-5.4")))
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	client, err := NewHardened(context.Background(), Config{
		Endpoint:   DefaultAPI,
		APIKey:     "tok",
		HTTPClient: &http.Client{Transport: rewriteHost{base: ts.URL, next: ts.Client().Transport}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.HTTPVersion() != HTTPVersion2 {
		t.Fatal(client.HTTPVersion())
	}
	models, err := client.DiscoverUsableModels(context.Background())
	if err != nil || len(models) != 1 || models[0] != "gpt-5.4" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if proto != 2 || path != usableModelsPath {
		t.Fatalf("proto=%d path=%s", proto, path)
	}
}

func TestVisionShrinksOversizedActiveImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2100, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	got, err := boundVisionPayload(dataURL, "auto")
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := splitDataURL(got)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > maxVisionEdge {
		t.Fatalf("cfg=%#v err=%v", cfg, err)
	}
}

func TestImageOnlyActiveTurnRemainsUserAction(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nxxxx")
	ok := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: ok}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	action := mustFields(t, encodeConversationAction(req))
	if len(fieldBytes(action, 1)) == 0 {
		t.Fatal("image-only turn must remain a user action")
	}
}

func fmtInvalid() error {
	return errors.Join(ErrInvalidArgument, io.EOF)
}
