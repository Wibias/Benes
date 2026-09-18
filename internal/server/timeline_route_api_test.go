package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/timeline"
)

func TestRequestTimelineExposesRouteAttribution(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	tr := timeline.New("req-route-api", 8)
	tr.SetRoute(timeline.Route{RequestedProvider: "openai", ProviderConnection: "openai-apikey", Model: "gpt-5.6"})
	tr.Mark(timeline.StagePreDispatch, timeline.SideLocal, timeline.MilestoneDispatch, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}

	h := &handler{timeline: store}
	req := httptest.NewRequest(http.MethodGet, "/v1/request-timeline?id=req-route-api", nil)
	rr := httptest.NewRecorder()
	h.handleRequestTimeline(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Route struct {
			RequestedProvider  string `json:"requestedProvider"`
			ProviderConnection string `json:"providerConnection"`
			Model              string `json:"model"`
		} `json:"route"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Route.RequestedProvider != "openai" || body.Route.ProviderConnection != "openai-apikey" || body.Route.Model != "gpt-5.6" {
		t.Fatalf("route=%#v", body.Route)
	}
}
