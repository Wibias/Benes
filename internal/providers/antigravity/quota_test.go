package antigravity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseQuotaPreservesZeroAndRejectsBadBodies(t *testing.T) {
	live, err := ParseQuota([]byte(`{"models":{"gemini-3.7-flash":{"displayName":"Gemini","quotaInfo":[{"tier":"Gem","remainingFraction":1.0}]},"claude-sonnet-4-6":{"displayName":"Claude","quotaInfo":[{"tier":"Cla","remainingPercentage":40}]}}}`), 0)
	if err != nil || len(live.Windows) != 2 {
		t.Fatalf("live=%#v err=%v", live, err)
	}
	var gem, cla *QuotaWindow
	for i := range live.Windows {
		switch live.Windows[i].Label {
		case "Gem":
			gem = &live.Windows[i]
		case "Cla":
			cla = &live.Windows[i]
		}
	}
	if gem == nil || gem.Percent != 0 {
		t.Fatalf("zero quota must remain data: %#v", gem)
	}
	if cla == nil || cla.Percent != 60 {
		t.Fatalf("claude=%#v", cla)
	}
	if _, err := ParseQuota([]byte(`{`), 0); !errors.Is(err, ErrQuotaUnreadable) {
		t.Fatalf("malformed=%v", err)
	}
	if _, err := ParseQuota([]byte(strings.Repeat("x", 32)), 16); !errors.Is(err, ErrQuotaOversized) {
		t.Fatalf("oversized=%v", err)
	}
}

func TestFetchQuotaKeepsDailyWhenSummaryWouldFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth=%q", r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"models":{"gemini-3.7-flash":{"displayName":"Gemini","quotaInfo":[{"remainingFraction":0.5}]}}}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	got, err := FetchQuota(context.Background(), client, DailyAPI, "tok", "proj")
	if err != nil || len(got.Windows) != 1 || got.Windows[0].Percent != 50 {
		t.Fatalf("fetch=%#v err=%v", got, err)
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

func TestMergeQuotaPrefersLiveEvidence(t *testing.T) {
	live := Quota{Source: "live", Windows: []QuotaWindow{{Label: "Gem", Percent: 10}}}
	catalog := Quota{Source: "catalog", Windows: []QuotaWindow{{Label: "Gem", Percent: 90}}}
	got := MergeQuota(live, catalog)
	if got.Source != "live" || got.Windows[0].Percent != 10 {
		t.Fatalf("merge=%#v", got)
	}
	got = MergeQuota(Quota{Source: "empty"}, catalog)
	if got.Source != "catalog" {
		t.Fatalf("catalog fallback=%#v", got)
	}
}
