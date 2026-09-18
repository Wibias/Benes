package transport

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestMatchNoProxyBypassesLoopbackSuffixAndExactHosts(t *testing.T) {
	target := mustURL(t, "https://api.example.com:443/v1")
	if !MatchNoProxy(mustURL(t, "http://127.0.0.1:8080"), nil) {
		t.Fatal("loopback must bypass")
	}
	if !MatchNoProxy(mustURL(t, "https://localhost/v1"), nil) {
		t.Fatal("localhost must bypass")
	}
	if !MatchNoProxy(target, []string{"EXAMPLE.com"}) {
		t.Fatal("exact host")
	}
	if !MatchNoProxy(mustURL(t, "https://chat.example.com"), []string{".example.com"}) {
		t.Fatal("suffix host")
	}
	if MatchNoProxy(target, []string{"api.example.com:80"}) {
		t.Fatal("port mismatch should not match")
	}
	if !MatchNoProxy(target, []string{"*"}) {
		t.Fatal("* should match")
	}
}

func TestPolicyFromConfigUsesFixedURLWithoutMutatingEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://env-proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://env-proxy.example:8080")
	policy := PolicyFromConfig("http://fixed-proxy.example:3128", "internal.test")
	if policy == nil || policy.Mode != ProxyModeFixed {
		t.Fatalf("mode=%#v", policy)
	}
	req := &http.Request{URL: mustURL(t, "https://api.openai.com/v1/responses")}
	got, err := policy.Select(req)
	if err != nil || got == nil || got.Host != "fixed-proxy.example:3128" {
		t.Fatalf("fixed=%v err=%v", got, err)
	}
	bypass, err := policy.Select(&http.Request{URL: mustURL(t, "https://internal.test/v1")})
	if err != nil || bypass != nil {
		t.Fatalf("noProxy=%v err=%v", bypass, err)
	}
	direct, err := policy.Select(&http.Request{URL: mustURL(t, "http://127.0.0.1:18790/v1")})
	if err != nil || direct != nil {
		t.Fatalf("loopback=%v err=%v", direct, err)
	}
	if got := os.Getenv("HTTP_PROXY"); got != "http://env-proxy.example:8080" {
		t.Fatalf("env mutated: %q", got)
	}
}

func TestPolicyFromConfigEnvReferenceUsesEnvironmentMode(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://env-proxy.example:8080")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	policy := PolicyFromConfig("${HTTPS_PROXY}", "")
	if policy == nil || policy.Mode != ProxyModeEnvironment {
		t.Fatalf("mode=%#v", policy)
	}
	got, err := policy.Select(&http.Request{URL: mustURL(t, "https://api.openai.com/v1")})
	if err != nil || got == nil || got.Host != "env-proxy.example:8080" {
		t.Fatalf("env proxy=%v err=%v", got, err)
	}
}

func TestDirectProxyPolicyNeverSelectsAProxy(t *testing.T) {
	policy := ProxyPolicy{Mode: ProxyModeDirect}
	got, err := policy.Select(&http.Request{URL: mustURL(t, "https://api.openai.com/v1")})
	if err != nil || got != nil {
		t.Fatalf("direct=%v err=%v", got, err)
	}
}

func TestPolicyFromConfigSystemSelectsOSDiscoveryMode(t *testing.T) {
	policy := PolicyFromConfig("system", "")
	if policy == nil || policy.Mode != ProxyModeSystem {
		t.Fatalf("mode=%#v", policy)
	}
	auto := PolicyFromConfig("auto", "")
	if auto == nil || auto.Mode != ProxyModeSystem {
		t.Fatalf("auto=%#v", auto)
	}
}

func TestSystemProxyRefreshesAfterTTLAndKeepsLastGoodOnError(t *testing.T) {
	lookups := 0
	now := time.Unix(0, 0)
	policy := &ProxyPolicy{
		Mode: ProxyModeSystem,
		now:  func() time.Time { return now },
		lookupSystem: func() (string, []string, error) {
			lookups++
			if lookups == 1 {
				return "http://sys-proxy.example:8080", nil, nil
			}
			if lookups == 2 {
				return "http://sys-proxy-2.example:8080", nil, nil
			}
			return "", nil, errors.New("registry unavailable")
		},
	}
	req := &http.Request{URL: mustURL(t, "https://api.openai.com/v1")}
	first, err := policy.Select(req)
	if err != nil || first == nil || first.Host != "sys-proxy.example:8080" {
		t.Fatalf("first=%v err=%v", first, err)
	}
	second, err := policy.Select(req)
	if err != nil || second == nil || second.Host != "sys-proxy.example:8080" || lookups != 1 {
		t.Fatalf("ttl reuse lookups=%d host=%v", lookups, second)
	}
	now = now.Add(31 * time.Second)
	third, err := policy.Select(req)
	if err != nil || third == nil || third.Host != "sys-proxy-2.example:8080" || lookups != 2 {
		t.Fatalf("refresh=%v lookups=%d err=%v", third, lookups, err)
	}
	now = now.Add(31 * time.Second)
	fourth, err := policy.Select(req)
	if err != nil || fourth == nil || fourth.Host != "sys-proxy-2.example:8080" || lookups != 3 {
		t.Fatalf("hysteresis=%v lookups=%d err=%v", fourth, lookups, err)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
