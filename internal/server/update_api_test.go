package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateAPIIsRetiredOnLoopback(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusGone || !strings.Contains(rr.Body.String(), "self-update retired") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	badge := httptest.NewRequest(http.MethodGet, "/api/update/badge", nil)
	badge.Host = "127.0.0.1"
	badgeRR := httptest.NewRecorder()
	h.ServeHTTP(badgeRR, badge)
	if badgeRR.Code != http.StatusOK || !strings.Contains(badgeRR.Body.String(), `"canUpdate":false`) {
		t.Fatalf("badge status=%d body=%s", badgeRR.Code, badgeRR.Body.String())
	}
}
