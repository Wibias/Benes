package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/provideractivity"
	"github.com/Wibias/Benes/internal/quota"
)

func TestProvidersWorkspaceAttentionDropsWhenLiveStateRecovers(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"providers": {
			"local": {"adapter":"local","baseUrl":"http://127.0.0.1:11434","authMode":"local"}
		}
	}`, "local")
	inner := h.(*handler)
	used := 12.0

	cases := []struct {
		name   string
		events []provideractivity.Event
		quota  []quota.Report
		want   string
	}{
		{
			name: "rate_limited then healthy quota report",
			events: []provideractivity.Event{
				{Provider: "local", Type: "rate_limited", Severity: "warn", Timestamp: 5000},
			},
			quota: []quota.Report{{
				Provider: "local", UpdatedAt: 1000,
				Quota: quota.Quota{WeeklyPercent: &used, UpdatedAt: 1000},
			}},
			want: "healthy",
		},
		{
			name: "quota_exhausted then later validation",
			events: []provideractivity.Event{
				{Provider: "local", Type: "quota_exhausted", Severity: "warn", Timestamp: 1000},
				{Provider: "local", Type: "connection_validated", Severity: "info", Timestamp: 2000},
			},
			want: "healthy",
		},
		{
			name: "health failure then later validation",
			events: []provideractivity.Event{
				{Provider: "local", Type: "provider_health_failure", Severity: "error", Timestamp: 1000},
				{Provider: "local", Type: "connection_validated", Severity: "info", Timestamp: 2000},
			},
			want: "healthy",
		},
		{
			name: "stale catalogue then later sync",
			events: []provideractivity.Event{
				{Provider: "local", Type: "model_catalogue_stale", Severity: "warn", Timestamp: 1000},
				{Provider: "local", Type: "model_catalogue_synchronized", Severity: "info", Timestamp: 2000},
			},
			want: "healthy",
		},
		{
			name: "health failure then later catalogue sync",
			events: []provideractivity.Event{
				{Provider: "local", Type: "provider_health_failure", Severity: "error", Timestamp: 1000},
				{Provider: "local", Type: "model_catalogue_synchronized", Severity: "info", Timestamp: 2000},
			},
			want: "healthy",
		},
		{
			name: "unrecovered health failure stays",
			events: []provideractivity.Event{
				{Provider: "local", Type: "provider_health_failure", Severity: "error", Timestamp: 1000},
			},
			want: "attention",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner.activity = provideractivity.New()
			for _, event := range tc.events {
				inner.activity.Record(event)
			}
			if tc.quota != nil {
				inner.codexQuota = func() []quota.Report { return tc.quota }
			} else {
				inner.codexQuota = nil
			}
			lifecycle, _ := readNamedWorkspaceLifecycle(t, h, "local")
			if lifecycle != tc.want {
				t.Fatalf("lifecycle=%q want %q", lifecycle, tc.want)
			}
		})
	}
}

func readNamedWorkspaceLifecycle(t *testing.T, h http.Handler, id string) (string, []string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Providers []struct {
			ID        string `json:"id"`
			Lifecycle string `json:"lifecycle"`
		} `json:"providers"`
		Attention []struct {
			Provider string `json:"provider"`
			Code     string `json:"code"`
		} `json:"attention"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	lifecycle := ""
	for _, provider := range body.Providers {
		if provider.ID == id {
			lifecycle = provider.Lifecycle
			break
		}
	}
	codes := make([]string, 0)
	for _, issue := range body.Attention {
		if issue.Provider == id {
			codes = append(codes, issue.Code)
		}
	}
	return lifecycle, codes
}
