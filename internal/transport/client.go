package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ProxyFunc func(*http.Request) (*url.URL, error)

type ClientOptions struct {
	DialTimeout            time.Duration
	KeepAlive              time.Duration
	MaxIdleConns           int
	MaxIdleConnsPerHost    int
	MaxConnsPerHost        int
	IdleConnTimeout        time.Duration
	TLSHandshakeTimeout    time.Duration
	ResponseHeaderTimeout  time.Duration
	MaxResponseHeaderBytes int64
	MaxRequestBodyBytes    int64
	RequestBodyLimitPath   string
	TLSConfig              *tls.Config
	Proxy                  ProxyFunc
	Pacer                  *Pacer
	PaceKey                func(*http.Request) string
	PaceInterval           func(*http.Request) time.Duration
	PaceLabel              func(*http.Request) string
	AllowDirectFallback    bool
}

func NewPinnedClient(target Target, options ClientOptions) *http.Client {
	options.Proxy = nil
	return NewClient(target, options)
}

func NewClient(target Target, options ClientOptions) *http.Client {
	options = withClientDefaults(options)
	dialer := &net.Dialer{Timeout: options.DialTimeout, KeepAlive: options.KeepAlive}

	directTLS := cloneTLSConfig(options.TLSConfig)
	directTLS.ServerName = target.ServerName
	direct := newTransport(options, nil, pinnedDialContext(dialer, target), directTLS)

	var roundTripper http.RoundTripper = targetRoundTripper{target: target, next: direct}
	if options.Proxy != nil {
		proxyTLS := cloneTLSConfig(options.TLSConfig)
		roundTripper = &proxyAwareRoundTripper{
			target:    target,
			proxy:     options.Proxy,
			direct:    direct,
			options:   options,
			dialer:    dialer,
			tlsConfig: proxyTLS,
		}
	}

	if options.Pacer != nil {
		key := options.PaceKey
		if key == nil {
			key = func(request *http.Request) string {
				if request == nil || request.URL == nil {
					return ""
				}
				return request.URL.Scheme + "://" + request.URL.Host + request.URL.Path
			}
		}
		roundTripper = pacingRoundTripper{
			pacer: options.Pacer, key: key, interval: options.PaceInterval,
			label: options.PaceLabel, next: roundTripper,
		}
	}
	roundTripper = Observe(roundTripper)
	roundTripper = withRequestBodyLimit(roundTripper, options.MaxRequestBodyBytes, options.RequestBodyLimitPath)

	return &http.Client{
		Transport: roundTripper,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func NewUnpinnedClient(options ClientOptions) *http.Client {
	options = withClientDefaults(options)
	if options.Proxy == nil {
		options.Proxy = PolicyFromConfig("", "").Func()
	}
	dialer := &net.Dialer{Timeout: options.DialTimeout, KeepAlive: options.KeepAlive}
	roundTripper := http.RoundTripper(newTransport(options, options.Proxy, dialer.DialContext, cloneTLSConfig(options.TLSConfig)))
	if options.Pacer != nil {
		roundTripper = pacingRoundTripper{
			pacer: options.Pacer, key: options.PaceKey, interval: options.PaceInterval,
			label: options.PaceLabel, next: roundTripper,
		}
	}
	roundTripper = Observe(roundTripper)
	roundTripper = withRequestBodyLimit(roundTripper, options.MaxRequestBodyBytes, options.RequestBodyLimitPath)
	return &http.Client{
		Transport: roundTripper,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var (
	unpinnedOnce   sync.Once
	unpinnedClient *http.Client
)

func DefaultUnpinnedClient() *http.Client {
	unpinnedOnce.Do(func() {
		unpinnedClient = NewUnpinnedClient(ClientOptions{})
	})
	return unpinnedClient
}

type pacingRoundTripper struct {
	pacer    *Pacer
	key      func(*http.Request) string
	interval func(*http.Request) time.Duration
	label    func(*http.Request) string
	next     http.RoundTripper
}

func (r pacingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if r.pacer != nil {
		key := ""
		if r.key != nil {
			key = r.key(request)
		}
		interval := r.pacer.interval
		if r.interval != nil {
			interval = r.interval(request)
		}
		label := ""
		if r.label != nil {
			label = r.label(request)
		}
		if err := r.pacer.AcquireWith(request.Context(), key, interval, label); err != nil {
			return nil, err
		}
	}
	return r.next.RoundTrip(request)
}

func cloneTLSConfig(source *tls.Config) *tls.Config {
	var config *tls.Config
	if source == nil {
		config = &tls.Config{}
	} else {
		config = source.Clone()
	}
	if config.MinVersion == 0 || config.MinVersion < tls.VersionTLS12 {
		config.MinVersion = tls.VersionTLS12
	}
	return config
}

func newTransport(options ClientOptions, proxy ProxyFunc, dialContext func(context.Context, string, string) (net.Conn, error), tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{
		Proxy:                  proxy,
		DialContext:            dialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           options.MaxIdleConns,
		MaxIdleConnsPerHost:    options.MaxIdleConnsPerHost,
		MaxConnsPerHost:        options.MaxConnsPerHost,
		IdleConnTimeout:        options.IdleConnTimeout,
		TLSHandshakeTimeout:    options.TLSHandshakeTimeout,
		ResponseHeaderTimeout:  options.ResponseHeaderTimeout,
		MaxResponseHeaderBytes: options.MaxResponseHeaderBytes,
		TLSClientConfig:        tlsConfig,
	}
}

type targetRoundTripper struct {
	target Target
	next   http.RoundTripper
}

func (r targetRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if !r.target.matches(request.URL) {
		return nil, destinationMismatchError(request, r.target)
	}
	if r.target.ResolutionDeferred {
		return nil, deferredResolutionError(r.target)
	}
	return r.next.RoundTrip(request)
}

type proxyAwareRoundTripper struct {
	target    Target
	proxy     ProxyFunc
	direct    http.RoundTripper
	options   ClientOptions
	dialer    *net.Dialer
	tlsConfig *tls.Config
	proxies   sync.Map
}

func (r *proxyAwareRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if !r.target.matches(request.URL) {
		return nil, destinationMismatchError(request, r.target)
	}
	proxyURL, err := r.proxy(request)
	if err != nil {
		return nil, fmt.Errorf("resolve proxy: %w", err)
	}
	if proxyURL == nil {
		if r.target.ResolutionDeferred {
			return nil, deferredResolutionError(r.target)
		}
		return r.direct.RoundTrip(request)
	}
	if r.target.PrivateNetwork {
		return nil, fmt.Errorf("private-network destination %s://%s:%s must bypass the configured proxy via NO_PROXY", r.target.Scheme, r.target.ServerName, r.target.Port)
	}
	response, err := r.transportForProxy(proxyURL).RoundTrip(request)
	if err != nil && r.options.AllowDirectFallback && isProxyConnectError(err) {
		return r.direct.RoundTrip(request)
	}
	return response, err

}

func isProxyConnectError(err error) bool {
	if err == nil {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "proxyconnect")
}

func (r *proxyAwareRoundTripper) transportForProxy(proxyURL *url.URL) http.RoundTripper {
	key := proxyURL.String()
	if existing, ok := r.proxies.Load(key); ok {
		return existing.(http.RoundTripper)
	}
	proxyCopy := *proxyURL
	transport := newTransport(
		r.options,
		ProxyFunc(http.ProxyURL(&proxyCopy)),
		r.dialer.DialContext,
		r.tlsConfig.Clone(),
	)
	actual, _ := r.proxies.LoadOrStore(key, http.RoundTripper(transport))
	return actual.(http.RoundTripper)
}

func destinationMismatchError(request *http.Request, target Target) error {
	return fmt.Errorf("request destination %q does not match pinned target %s://%s:%s", request.URL, target.Scheme, target.ServerName, target.Port)
}

func deferredResolutionError(target Target) error {
	return fmt.Errorf("destination %s://%s:%s requires proxy DNS resolution but the proxy selector chose a direct connection", target.Scheme, target.ServerName, target.Port)
}

func pinnedDialContext(dialer *net.Dialer, target Target) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var lastErr error
		for _, address := range target.Addresses {
			if network == "tcp4" && !address.Is4() {
				continue
			}
			if network == "tcp6" && !address.Is6() {
				continue
			}
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), target.Port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no pinned address supports network %q", network)
		}
		return nil, lastErr
	}
}

func withClientDefaults(options ClientOptions) ClientOptions {
	if options.DialTimeout <= 0 {
		options.DialTimeout = 10 * time.Second
	}
	if options.KeepAlive <= 0 {
		options.KeepAlive = 30 * time.Second
	}
	if options.MaxIdleConns <= 0 {
		options.MaxIdleConns = 100
	}
	if options.MaxIdleConnsPerHost <= 0 {
		options.MaxIdleConnsPerHost = 16
	}
	if options.MaxConnsPerHost <= 0 {
		options.MaxConnsPerHost = 64
	}
	if options.IdleConnTimeout <= 0 {
		options.IdleConnTimeout = 90 * time.Second
	}
	if options.TLSHandshakeTimeout <= 0 {
		options.TLSHandshakeTimeout = 10 * time.Second
	}
	if options.ResponseHeaderTimeout <= 0 {
		options.ResponseHeaderTimeout = 300 * time.Second
	}
	if options.MaxResponseHeaderBytes <= 0 {
		options.MaxResponseHeaderBytes = 1 << 20
	}
	return options
}
