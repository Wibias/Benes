package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPServerUsesStreamingSafeBoundedDefaults(t *testing.T) {
	server, shutdownTimeout := newHTTPServer(context.Background(), http.NotFoundHandler(), HTTPServerOptions{})
	if server.ReadHeaderTimeout <= 0 {
		t.Fatalf("ReadHeaderTimeout=%v", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout <= 0 {
		t.Fatalf("ReadTimeout=%v", server.ReadTimeout)
	}
	if server.IdleTimeout <= 0 {
		t.Fatalf("IdleTimeout=%v", server.IdleTimeout)
	}
	if server.MaxHeaderBytes <= 0 {
		t.Fatalf("MaxHeaderBytes=%d", server.MaxHeaderBytes)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout=%v; streamed responses must not have a global write deadline", server.WriteTimeout)
	}
	if shutdownTimeout <= 0 {
		t.Fatalf("shutdownTimeout=%v", shutdownTimeout)
	}
}

func TestServeHTTPServesPreboundListenerAndShutsDownOnContextCancel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}), HTTPServerOptions{})
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		cancel()
		t.Fatalf("status=%d", response.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeHTTP()=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not stop after context cancellation")
	}
}

func TestServeHTTPForceClosesAfterShutdownDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			_, _ = io.WriteString(w, "late")
		}), HTTPServerOptions{ShutdownTimeout: 40 * time.Millisecond})
	}()

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("handler was not entered")
	}

	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "shutdown") {
			close(release)
			t.Fatalf("ServeHTTP()=%v", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("ServeHTTP did not force close after shutdown deadline")
	}
	close(release)
	select {
	case <-clientDone:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not unblock after force close")
	}
}

func TestServeHTTPPropagatesListenerFailure(t *testing.T) {
	sentinel := errors.New("accept failed")
	err := ServeHTTP(context.Background(), errorListener{err: sentinel}, http.NotFoundHandler(), HTTPServerOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ServeHTTP()=%v, want sentinel", err)
	}
}

func TestServeHTTPRejectsNilDependencies(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := ServeHTTP(context.Background(), nil, http.NotFoundHandler(), HTTPServerOptions{}); err == nil {
		t.Fatal("nil listener accepted")
	}
	if err := ServeHTTP(context.Background(), listener, nil, HTTPServerOptions{}); err == nil {
		t.Fatal("nil handler accepted")
	}
}

type errorListener struct{ err error }

func (l errorListener) Accept() (net.Conn, error) { return nil, l.err }
func (l errorListener) Close() error              { return nil }
func (l errorListener) Addr() net.Addr            { return staticAddr("test") }

type staticAddr string

func (a staticAddr) Network() string { return string(a) }
func (a staticAddr) String() string  { return string(a) }
