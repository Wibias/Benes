package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
	runtimecore "github.com/Wibias/Benes/internal/runtime"
)

func TestServeDataPlaneBindsServesAndStopsOnCancel(t *testing.T) {
	actual, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	requested := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	plane := DataPlane{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }), Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}, bindReady: true}
	done := make(chan error, 1)
	go func() {
		done <- ServeDataPlane(ctx, plane, ServeOptions{Bind: BindOptions{Listen: func(network, address string) (net.Listener, error) {
			if network != "tcp" {
				t.Errorf("network=%q", network)
			}
			requested <- address
			return actual, nil
		}}})
	}()
	select {
	case got := <-requested:
		if got != "127.0.0.1:23100" {
			t.Fatalf("address=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("bind not attempted")
	}
	resp, err := http.Get("http://" + actual.Addr().String())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		cancel()
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeDataPlane=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func TestServeDataPlaneRejectsNilContextBeforeBind(t *testing.T) {
	called := false
	plane := DataPlane{Handler: http.NotFoundHandler(), Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}, bindReady: true}
	err := ServeDataPlane(nil, plane, ServeOptions{Bind: BindOptions{Listen: func(string, string) (net.Listener, error) { called = true; return nil, nil }}})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestServeDataPlanePropagatesRuntimeServeFailureAndCloses(t *testing.T) {
	sentinel := errors.New("accept exploded")
	listener := &serveErrorListener{err: sentinel}
	plane := DataPlane{Handler: http.NotFoundHandler(), Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}, bindReady: true}
	err := ServeDataPlane(context.Background(), plane, ServeOptions{Bind: BindOptions{Listen: func(string, string) (net.Listener, error) { return listener, nil }}, HTTP: runtimecore.HTTPServerOptions{}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	if listener.closes == 0 {
		t.Fatal("listener not closed")
	}
}

func TestServeDataPlaneAfterListenErrorAbortsServe(t *testing.T) {
	listener := &serveErrorListener{err: errors.New("should not accept")}
	plane := DataPlane{Handler: http.NotFoundHandler(), Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}, bindReady: true}
	err := ServeDataPlane(context.Background(), plane, ServeOptions{
		Bind: BindOptions{Listen: func(string, string) (net.Listener, error) { return listener, nil }},
		AfterListen: func(hostname string, port int) error {
			if hostname != "127.0.0.1" || port != 23100 {
				t.Fatalf("hostname=%s port=%d", hostname, port)
			}
			return errors.New("inject failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "inject failed") {
		t.Fatalf("err=%v", err)
	}
	if listener.closes == 0 {
		t.Fatal("listener not closed")
	}
}

type serveErrorListener struct {
	err    error
	closes int
}

func (l *serveErrorListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *serveErrorListener) Close() error              { l.closes++; return nil }
func (l *serveErrorListener) Addr() net.Addr            { return serveAddr("test") }

type serveAddr string

func (a serveAddr) Network() string { return string(a) }
func (a serveAddr) String() string  { return string(a) }

func TestServeDataPlanePublishesPidAndRuntimePortThenClears(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "benes.pid")
	portPath := filepath.Join(dir, "runtime-port.json")
	actual, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	plane := DataPlane{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}, bindReady: true}
	done := make(chan error, 1)
	go func() {
		done <- ServeDataPlane(ctx, plane, ServeOptions{
			Bind:            BindOptions{Listen: func(string, string) (net.Listener, error) { return actual, nil }},
			PIDPath:         pidPath,
			RuntimePortPath: portPath,
		})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(portPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("runtime-port file not written")
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if string(raw) != strconv.Itoa(os.Getpid()) {
		cancel()
		t.Fatalf("pid=%q", raw)
	}
	benesPid, err := os.ReadFile(filepath.Join(dir, "benes.pid"))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if string(benesPid) != string(raw) {
		cancel()
		t.Fatalf("benes.pid=%q", benesPid)
	}
	portRaw, err := os.ReadFile(portPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if strings.Contains(string(portRaw), "secret") {
		cancel()
		t.Fatalf("secret in runtime-port: %s", portRaw)
	}
	var state struct {
		PID      int    `json:"pid"`
		Port     int    `json:"port"`
		Hostname string `json:"hostname"`
	}
	if err := json.Unmarshal(portRaw, &state); err != nil {
		cancel()
		t.Fatal(err)
	}
	if state.PID != os.Getpid() || state.Hostname != "127.0.0.1" || state.Port < 1 {
		cancel()
		t.Fatalf("state=%#v", state)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeDataPlane=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop")
	}
	if _, err := os.Stat(pidPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid remained: %v", err)
	}
	if _, err := os.Stat(portPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime-port remained: %v", err)
	}
}

func TestServeDataPlaneBindFailureDoesNotStartStorage(t *testing.T) {
	started := 0
	plane := DataPlane{
		Handler:      http.NotFoundHandler(),
		Listener:     config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100},
		bindReady:    true,
		startStorage: func(context.Context) { started++ },
	}
	err := ServeDataPlane(context.Background(), plane, ServeOptions{Bind: BindOptions{Listen: func(string, string) (net.Listener, error) {
		return nil, errors.New("bind failed")
	}}})
	if err == nil {
		t.Fatal("expected bind failure")
	}
	if started != 0 {
		t.Fatalf("startup cleanup ran on bind failure: %d", started)
	}
}

func TestServeDataPlaneAfterListenFailureDoesNotStartStorage(t *testing.T) {
	started := 0
	listener := &serveErrorListener{err: errors.New("should not accept")}
	plane := DataPlane{
		Handler:      http.NotFoundHandler(),
		Listener:     config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100},
		bindReady:    true,
		startStorage: func(context.Context) { started++ },
	}
	err := ServeDataPlane(context.Background(), plane, ServeOptions{
		Bind: BindOptions{Listen: func(string, string) (net.Listener, error) { return listener, nil }},
		AfterListen: func(string, int) error {
			return errors.New("inject failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "inject failed") {
		t.Fatalf("err=%v", err)
	}
	if started != 0 {
		t.Fatalf("storage scheduler started after AfterListen failure: %d", started)
	}
}

func TestServeDataPlaneStartsStorageOnceAndCancelsWithParent(t *testing.T) {
	actual, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	got := make(chan context.Context, 1)
	plane := DataPlane{
		Handler:   http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Listener:  config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100},
		bindReady: true,
		startStorage: func(ctx context.Context) {
			started++
			got <- ctx
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeDataPlane(ctx, plane, ServeOptions{Bind: BindOptions{Listen: func(string, string) (net.Listener, error) { return actual, nil }}})
	}()
	var storageCtx context.Context
	select {
	case storageCtx = <-got:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("storage did not start after listen")
	}
	if started != 1 {
		cancel()
		t.Fatalf("started=%d", started)
	}
	cancel()
	select {
	case <-storageCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("storage context survived parent cancel")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeDataPlane=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop")
	}
}
