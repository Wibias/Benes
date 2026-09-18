package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubStarAPIRefusesIdentitySpend(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/github/star", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/github/star", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"unknown":true`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/api/github/star", nil)
	post.Host = "127.0.0.1"
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusForbidden {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
}
