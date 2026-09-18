package bootstrap

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func readyPlane(host string, port int) DataPlane {
	return DataPlane{
		Handler:   http.NotFoundHandler(),
		Listener:  config.ListenerConfig{Hostname: host, Port: port},
		bindReady: true,
	}
}

func TestBindDataPlaneUsesJoinHostPortForIPv4AndIPv6(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 23100, "127.0.0.1:23100"},
		{"0.0.0.0", 20200, "0.0.0.0:20200"},
		{"::1", 30300, "[::1]:30300"},
		{"::", 40400, "[::]:40400"},
		{"gateway.internal", 50500, "gateway.internal:50500"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			var network, address string
			sentinel := &stubListener{}
			got, err := BindDataPlane(readyPlane(tc.host, tc.port), BindOptions{Listen: func(n, a string) (net.Listener, error) {
				network, address = n, a
				return sentinel, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got != sentinel {
				t.Fatalf("listener=%T want sentinel", got)
			}
			if network != "tcp" || address != tc.want {
				t.Fatalf("network=%q address=%q want tcp %q", network, address, tc.want)
			}
		})
	}
}

func TestBindDataPlaneRejectsUnbuiltOrInvalidPlaneBeforeListen(t *testing.T) {
	called := false
	listen := func(string, string) (net.Listener, error) {
		called = true
		return &stubListener{}, nil
	}
	cases := []DataPlane{
		{Handler: http.NotFoundHandler(), Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}},
		{Handler: nil, Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}, bindReady: true},
		readyPlane("", 23100),
		readyPlane("  ", 23100),
		readyPlane("127.0.0.1", 0),
		readyPlane("127.0.0.1", 65536),
	}
	for i, plane := range cases {
		called = false
		if _, err := BindDataPlane(plane, BindOptions{Listen: listen}); err == nil {
			t.Fatalf("case %d accepted", i)
		}
		if called {
			t.Fatalf("case %d called listen before validation", i)
		}
	}
}

func TestBindDataPlanePropagatesBindFailure(t *testing.T) {
	sentinel := errors.New("address already in use")
	_, err := BindDataPlane(readyPlane("127.0.0.1", 23100), BindOptions{Listen: func(network, address string) (net.Listener, error) {
		if network != "tcp" || address != "127.0.0.1:23100" {
			t.Fatalf("%s %s", network, address)
		}
		return nil, sentinel
	}})
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "bind data plane") {
		t.Fatalf("err=%v", err)
	}
}

func TestBindDataPlaneRejectsNilListenerResult(t *testing.T) {
	_, err := BindDataPlane(readyPlane("127.0.0.1", 23100), BindOptions{Listen: func(string, string) (net.Listener, error) {
		return nil, nil
	}})
	if err == nil {
		t.Fatal("nil listener accepted")
	}
}

type stubListener struct{}

func (*stubListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (*stubListener) Close() error              { return nil }
func (*stubListener) Addr() net.Addr            { return stubAddr("stub") }

type stubAddr string

func (a stubAddr) Network() string { return string(a) }
func (a stubAddr) String() string  { return string(a) }
