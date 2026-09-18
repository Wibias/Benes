package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/timeline"
)

func TestRequestTimelineLooksUpASavedTraceWithoutManagementAuth(t *testing.T) {
	dir := t.TempDir()
	store := timeline.NewStore(dir, 8)
	tr := timeline.New("req-op", 8)
	tr.Mark(timeline.StagePreDispatch, timeline.SideLocal, timeline.MilestoneDispatch, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"p": &fakeProvider{}},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/v1/request-timeline?id=req-op", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		ID     string `json:"id"`
		Events []struct {
			Stage     string `json:"stage"`
			OK        bool   `json:"ok"`
			Milestone string `json:"milestone"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != "req-op" || len(body.Events) == 0 || body.Events[0].Stage != string(timeline.StagePreDispatch) {
		t.Fatalf("body=%#v", body)
	}

	missing := httptest.NewRequest(http.MethodGet, "/v1/request-timeline?id=missing", nil)
	missing.Header.Set("Authorization", "Bearer local-secret")
	missRR := httptest.NewRecorder()
	h.ServeHTTP(missRR, missing)
	if missRR.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missRR.Code)
	}
}
