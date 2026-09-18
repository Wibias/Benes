package timeline

import "testing"

func TestTraceRouteRoundTripsThroughStore(t *testing.T) {
	store := NewStore(t.TempDir(), 8)
	tr := New("req-route", 8)
	tr.SetRoute(Route{
		RequestedProvider:  "openai",
		ProviderConnection: "openai-apikey",
		Model:              "gpt-5.6",
	})
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("req-route")
	if err != nil {
		t.Fatal(err)
	}
	route := got.Route()
	if route.RequestedProvider != "openai" || route.ProviderConnection != "openai-apikey" || route.Model != "gpt-5.6" {
		t.Fatalf("route=%#v", route)
	}
}

func TestTraceRouteDefaultsEmptyForLegacyTrace(t *testing.T) {
	tr := New("legacy", 4)
	if got := tr.Route(); got != (Route{}) {
		t.Fatalf("route=%#v", got)
	}
}
