package cursor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func cursorDispatch() providers.DispatchRequest {
	return providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{
			SystemPrompt: []string{"sys"},
			Messages:     []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
	}}
}

func http2TestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewUnstartedServer(handler)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	base := ts.Client()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:   DefaultAPI,
		APIKey:     "tok",
		HTTPClient: &http.Client{Transport: rewriteHost{base: ts.URL, next: base.Transport}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpVersion != HTTPVersion2 {
		t.Fatalf("default transport=%q", client.httpVersion)
	}
	return client, ts
}

func TestHTTP2WritesGetBlobOnSameStream(t *testing.T) {
	compiled, err := CompileRun(cursorDispatch().Parsed)
	if err != nil {
		t.Fatal(err)
	}
	roots := RootPromptBlobs(compiled)
	args := EncodeProtoMessage(4, append(EncodeProtoVarint(1, 7), EncodeProtoMessage(2, EncodeProtoBytes(1, BlobID(roots[0])))...))
	ask, err := EncodeConnectFrame(args, false)
	if err != nil {
		t.Fatal(err)
	}
	text, err := EncodeConnectFrame(EncodeProtoMessage(1, EncodeProtoMessage(1, EncodeProtoString(1, "ok"))), false)
	if err != nil {
		t.Fatal(err)
	}
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var proto atomic.Int32
	var replies [][]byte
	var mu sync.Mutex
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		proto.Store(int32(r.ProtoMajor))
		if r.URL.Path != runPath {
			t.Errorf("path=%s", r.URL.Path)
		}
		first := make([]byte, 64<<10)
		n, _ := r.Body.Read(first)
		if n == 0 {
			t.Error("missing initial run frame")
			return
		}
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(ask)
		if flusher != nil {
			flusher.Flush()
		}
		reply := make([]byte, 64<<10)
		n, err := r.Body.Read(reply)
		if err != nil && n == 0 {
			t.Errorf("same-stream read=%v", err)
			return
		}
		mu.Lock()
		replies = append(replies, append([]byte(nil), reply[:n]...))
		mu.Unlock()
		_, _ = w.Write(append(text, end...))
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	ev, err := got.Next()
	if err != nil || ev.Text != "ok" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	if proto.Load() != 2 {
		t.Fatalf("proto=%d", proto.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(replies) != 1 {
		t.Fatalf("replies=%d", len(replies))
	}
	frames, _, err := DecodeConnectFrames(replies[0])
	if err != nil || len(frames) == 0 {
		t.Fatalf("frames=%v err=%v", frames, err)
	}
	root, err := decodeProtoFields(frames[0].Payload)
	if err != nil || len(fieldBytes(root, 3)) == 0 {
		t.Fatalf("kv reply=%x err=%v", frames[0].Payload, err)
	}
}

func TestHTTP2MissingBlobFailsClosed(t *testing.T) {
	ask, _ := EncodeConnectFrame(EncodeProtoMessage(4, append(EncodeProtoVarint(1, 1), EncodeProtoMessage(2, EncodeProtoBytes(1, []byte("missing-blob-id-32-bytes-long!!")))...)), false)
	end, _ := EncodeConnectFrame(nil, true)
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(ask, end...))
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	ev, err := got.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != protocol.EventError {
		t.Fatalf("missing blob=%#v", ev)
	}
}

func TestHTTP2OversizedBlobReplyRejected(t *testing.T) {
	store := NewBlobStore()
	huge := make([]byte, maxLiveWriteBytes+8)
	id := store.Put(huge)
	ask, _ := EncodeConnectFrame(EncodeProtoMessage(4, append(EncodeProtoVarint(1, 1), EncodeProtoMessage(2, EncodeProtoBytes(1, id))...)), false)
	end, _ := EncodeConnectFrame(nil, true)
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(ask, end...))
	})
	client.blobs = store
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	ev, err := got.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != protocol.EventError {
		t.Fatalf("oversized=%#v", ev)
	}
}

func TestHTTP2WriteFailureAfterDispatchDoesNotReplay(t *testing.T) {
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0})
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if !client.session.Committed() {
		t.Fatal("dispatch must commit")
	}
	if err := client.session.MarkReachabilityFailure(); !errors.Is(err, ErrRequestCommitted) {
		t.Fatalf("replay=%v", err)
	}
}

func TestHTTP2NoWriteAfterClose(t *testing.T) {
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	cs := got.(*stream)
	if err := got.Close(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err := cs.writeLive([]byte("late")); !errors.Is(err, errNoWriteAfterTerminal) {
		t.Fatalf("late write=%v", err)
	}
}

func TestHTTP2CancelUnblocksWriter(t *testing.T) {
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	got, err := client.Open(ctx, cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = got.Close()
	}()
	cs := got.(*stream)
	blocker := &blockingPipeWriter{started: make(chan struct{}), closed: make(chan struct{})}
	cs.closeMu.Lock()
	cs.writer = blocker
	cs.closeMu.Unlock()
	done := make(chan error, 1)
	go func() { done <- cs.writeLive([]byte("blocked")) }()
	select {
	case <-blocker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("write never started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancel or closed write")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer stayed blocked")
	}
}

type blockingPipeWriter struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (w *blockingPipeWriter) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingPipeWriter) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func (w *blockingPipeWriter) CloseWithError(error) error {
	return w.Close()
}

func TestHTTP2OrderedBlobRequestsPreserveWriteOrder(t *testing.T) {
	compiled, err := CompileRun(cursorDispatch().Parsed)
	if err != nil {
		t.Fatal(err)
	}
	roots := RootPromptBlobs(compiled)
	if len(roots) == 0 {
		t.Fatal("expected root blobs")
	}
	first := EncodeProtoMessage(4, append(EncodeProtoVarint(1, 1), EncodeProtoMessage(2, EncodeProtoBytes(1, BlobID(roots[0])))...))
	second := EncodeProtoMessage(4, append(EncodeProtoVarint(1, 2), EncodeProtoMessage(2, EncodeProtoBytes(1, BlobID(roots[0])))...))
	ask1, err := EncodeConnectFrame(first, false)
	if err != nil {
		t.Fatal(err)
	}
	ask2, err := EncodeConnectFrame(second, false)
	if err != nil {
		t.Fatal(err)
	}
	text, err := EncodeConnectFrame(EncodeProtoMessage(1, EncodeProtoMessage(1, EncodeProtoString(1, "ok"))), false)
	if err != nil {
		t.Fatal(err)
	}
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var replies [][]byte
	var mu sync.Mutex
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		firstBuf := make([]byte, 64<<10)
		if n, _ := r.Body.Read(firstBuf); n == 0 {
			t.Error("missing initial run frame")
			return
		}
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(ask1)
		if flusher != nil {
			flusher.Flush()
		}
		reply := make([]byte, 64<<10)
		n, err := r.Body.Read(reply)
		if err != nil && n == 0 {
			t.Errorf("first reply=%v", err)
			return
		}
		mu.Lock()
		replies = append(replies, append([]byte(nil), reply[:n]...))
		mu.Unlock()
		_, _ = w.Write(ask2)
		if flusher != nil {
			flusher.Flush()
		}
		n, err = r.Body.Read(reply)
		if err != nil && n == 0 {
			t.Errorf("second reply=%v", err)
			return
		}
		mu.Lock()
		replies = append(replies, append([]byte(nil), reply[:n]...))
		mu.Unlock()
		_, _ = w.Write(append(text, end...))
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	ev, err := got.Next()
	if err != nil || ev.Text != "ok" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(replies) != 2 {
		t.Fatalf("replies=%d", len(replies))
	}
	var ids []uint64
	for _, raw := range replies {
		frames, _, err := DecodeConnectFrames(raw)
		if err != nil || len(frames) == 0 {
			t.Fatalf("frames=%v err=%v", frames, err)
		}
		root, err := decodeProtoFields(frames[0].Payload)
		if err != nil {
			t.Fatal(err)
		}
		kv, err := decodeProtoFields(fieldBytes(root, 3))
		if err != nil {
			t.Fatal(err)
		}
		id, ok := fieldVarint(kv, 1)
		if !ok {
			t.Fatalf("missing request id in %x", frames[0].Payload)
		}
		ids = append(ids, id)
	}
	if ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("order=%v", ids)
	}
}

func TestHTTP2ResponseEOFClosesWriter(t *testing.T) {
	end, err := EncodeConnectFrame(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(end)
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if _, err := got.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("eof=%v", err)
	}
	cs := got.(*stream)
	if err := cs.writeLive([]byte("late")); !errors.Is(err, errNoWriteAfterTerminal) {
		t.Fatalf("write after eof=%v", err)
	}
}

func TestHTTP2ConcurrentWritesStayOrdered(t *testing.T) {
	var replies [][]byte
	var mu sync.Mutex
	ready := make(chan struct{})
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		if n, _ := r.Body.Read(buf); n == 0 {
			t.Error("missing initial run frame")
			return
		}
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(ready)
		var acc []byte
		for len(replies) < 2 {
			n, err := r.Body.Read(buf)
			if err != nil && n == 0 {
				t.Errorf("reply read=%v", err)
				return
			}
			acc = append(acc, buf[:n]...)
			frames, rem, err := DecodeConnectFrames(acc)
			if err != nil {
				t.Errorf("decode=%v", err)
				return
			}
			acc = rem
			mu.Lock()
			for _, frame := range frames {
				replies = append(replies, append([]byte(nil), frame.Payload...))
			}
			mu.Unlock()
		}
	})
	got, err := client.Open(context.Background(), cursorDispatch())
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	<-ready
	cs := got.(*stream)
	first := EncodeGetBlobResult(1, []byte("one"))
	second := EncodeGetBlobResult(2, []byte("two"))
	errs := make(chan error, 2)
	go func() { errs <- cs.writeLive(first) }()
	go func() { errs <- cs.writeLive(second) }()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("write=%v", err)
		}
	}
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(replies)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("replies=%d", n)
		case <-time.After(10 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	var ids []uint64
	for _, raw := range replies {
		root, err := decodeProtoFields(raw)
		if err != nil {
			t.Fatalf("interleaved or corrupt frame %x err=%v", raw, err)
		}
		kv, err := decodeProtoFields(fieldBytes(root, 3))
		if err != nil {
			t.Fatal(err)
		}
		id, ok := fieldVarint(kv, 1)
		if !ok {
			t.Fatalf("missing id in %x", raw)
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 || (ids[0] != 1 && ids[0] != 2) || (ids[1] != 1 && ids[1] != 2) || ids[0] == ids[1] {
		t.Fatalf("ids=%v", ids)
	}
}

func TestHTTP2BudgetAccountsLiveWrites(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{ClassBytes: map[resourcebudget.Class]int64{
		resourcebudget.ClassDownstreamQueue: 16,
	}})
	turn, err := mgr.AcquireTurn(context.Background(), "cursor")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	client, _ := http2TestClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		_, _ = r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
	})
	got, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: cursorDispatch().Parsed, Turn: turn})
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	cs := got.(*stream)
	if err := cs.writeLive(make([]byte, 64)); !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("budget=%v", err)
	}
}
