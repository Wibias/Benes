package transport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPinnedClientDialsValidatedAddressAndPreservesHost(t *testing.T) {
	observedHost := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedHost <- r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := u.Port()
	target, err := ResolveTarget(context.Background(), "http://provider.test:"+port, DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewPinnedClient(target, ClientOptions{})
	req, _ := http.NewRequest(http.MethodGet, "http://provider.test:"+port+"/v1", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := <-observedHost; got != "provider.test:"+port {
		t.Fatalf("Host = %q, want provider.test:%s", got, port)
	}
}

func TestPinnedClientRefusesRedirects(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("redirect destination must not be reached")
	}))
	defer destination.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirector.Close()

	redirectURL, _ := url.Parse(redirector.URL)
	port := redirectURL.Port()
	target, err := ResolveTarget(context.Background(), "http://provider.test:"+port, DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewPinnedClient(target, ClientOptions{})
	req, _ := http.NewRequest(http.MethodGet, "http://provider.test:"+port+"/redirect", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.StatusCode)
	}
}

func TestPinnedClientPropagatesRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	target, err := ResolveTarget(context.Background(), "http://provider.test:"+u.Port(), DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://provider.test:"+u.Port(), nil)
	result := make(chan error, 1)
	go func() {
		response, err := NewPinnedClient(target, ClientOptions{}).Do(req)
		if response != nil {
			response.Body.Close()
		}
		result <- err
	}()

	<-started
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Do() returned nil error after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Do() did not return after request cancellation")
	}
}

func TestPinnedTLSClientPreservesOriginalServerName(t *testing.T) {
	certificate, roots := testCertificate(t, "provider.test")
	var observed atomic.Value

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{certificate},
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			observed.Store(info.ServerName)
			return nil, nil
		},
	})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	defer server.Close()
	go server.Serve(tlsListener)

	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	target, err := ResolveTarget(context.Background(), "https://provider.test:"+port, DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewPinnedClient(target, ClientOptions{TLSConfig: &tls.Config{RootCAs: roots}})
	req, _ := http.NewRequest(http.MethodGet, "https://provider.test:"+port, nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	response.Body.Close()

	if got, _ := observed.Load().(string); got != "provider.test" {
		t.Fatalf("TLS SNI = %q, want provider.test", got)
	}
}

func testCertificate(t *testing.T, dnsName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	return certificate, roots
}

func TestClientRejectsRequestForDifferentOrigin(t *testing.T) {
	target, err := ResolveTarget(context.Background(), "http://provider.test:8080", DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "http://different.test:8080", nil)
	response, err := NewClient(target, ClientOptions{}).Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("client accepted a request for an origin other than the pinned target")
	}
}

func TestClientUsesConfiguredProxyForPublicTarget(t *testing.T) {
	proxyRequests := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- r.RequestURI
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)

	target, err := ResolveTarget(context.Background(), "http://provider.test:8080", DestinationPolicy{
		Resolver: staticResolver{addrs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient(target, ClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}})
	request, _ := http.NewRequest(http.MethodGet, "http://provider.test:8080/v1/models", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := <-proxyRequests; got != "http://provider.test:8080/v1/models" {
		t.Fatalf("proxy RequestURI = %q", got)
	}
}

func TestClientRejectsPrivateTargetWhenProxyWouldHandleIt(t *testing.T) {
	proxyReached := atomic.Bool{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyReached.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)

	target, err := ResolveTarget(context.Background(), "http://provider.test:8080", DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "http://provider.test:8080", nil)
	response, err := NewClient(target, ClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}}).Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "NO_PROXY") {
		t.Fatalf("Do() error = %v, want private-proxy rejection", err)
	}
	if proxyReached.Load() {
		t.Fatal("private target was sent through the proxy")
	}
}

func TestClientEvaluatesProxySelectorOncePerRequest(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	var calls atomic.Int32

	target, err := ResolveTarget(context.Background(), "http://provider.test:8080", DestinationPolicy{
		Resolver: staticResolver{addrs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient(target, ClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
		calls.Add(1)
		return proxyURL, nil
	}})
	request, _ := http.NewRequest(http.MethodGet, "http://provider.test:8080", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	response.Body.Close()
	if calls.Load() != 1 {
		t.Fatalf("proxy selector calls = %d, want 1", calls.Load())
	}
}

func TestClientUsesProxyWhenTargetResolutionWasDeferred(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)

	target, err := ResolveTarget(context.Background(), "http://proxy-only.internal.example:8080", DestinationPolicy{
		Resolver:             staticResolver{err: &net.DNSError{Err: "no such host", Name: "proxy-only.internal.example", IsNotFound: true}},
		AllowProxyResolution: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "http://proxy-only.internal.example:8080/v1", nil)
	response, err := NewClient(target, ClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}}).Do(request)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	response.Body.Close()
}

func TestClientRejectsDeferredResolutionWhenProxyBypassesTarget(t *testing.T) {
	target, err := ResolveTarget(context.Background(), "http://proxy-only.internal.example:8080", DestinationPolicy{
		Resolver:             staticResolver{err: &net.DNSError{Err: "no such host", Name: "proxy-only.internal.example", IsNotFound: true}},
		AllowProxyResolution: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, "http://proxy-only.internal.example:8080/v1", nil)
	response, err := NewClient(target, ClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
		return nil, nil
	}}).Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "DNS resolution") {
		t.Fatalf("Do() error = %v, want deferred-resolution rejection", err)
	}
}

func TestProxyConnectFailureDoesNotFallbackByDefault(t *testing.T) {
	dead, _ := url.Parse("http://127.0.0.1:1")
	target, err := ResolveTarget(context.Background(), "http://provider.test:8080", DestinationPolicy{
		Resolver: staticResolver{addrs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(target, ClientOptions{
		DialTimeout: 200 * time.Millisecond,
		Proxy:       func(*http.Request) (*url.URL, error) { return dead, nil },
	})
	request, _ := http.NewRequest(http.MethodGet, "http://provider.test:8080/v1", nil)
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !isProxyConnectError(err) {
		t.Fatalf("Do() error = %v, want proxy connect failure", err)
	}
}

func TestProxyConnectFailureFallsBackWhenAllowed(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := originURL.Port()
	if port == "" {
		t.Fatal("origin port")
	}
	target := Target{
		Scheme:     "http",
		ServerName: originURL.Hostname(),
		Port:       port,
		Addresses:  []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}
	dead, _ := url.Parse("http://127.0.0.1:1")
	client := NewClient(target, ClientOptions{
		DialTimeout:         200 * time.Millisecond,
		AllowDirectFallback: true,
		Proxy:               func(*http.Request) (*url.URL, error) { return dead, nil },
	})
	request, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestNewUnpinnedClientUsesConfiguredProxy(t *testing.T) {
	proxyRequests := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- r.RequestURI
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := NewUnpinnedClient(ClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}})
	request, _ := http.NewRequest(http.MethodGet, "http://provider.test:8080/v1", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do(): %v", err)
	}
	response.Body.Close()
	if got := <-proxyRequests; got != "http://provider.test:8080/v1" {
		t.Fatalf("proxy RequestURI = %q", got)
	}
}
