package timeline

import "testing"

func TestMarkRecordsCurrentAttempt(t *testing.T) {
	tr := New("req-attempt", 8)
	tr.SetAttempt(2)
	tr.Mark(StageUpstreamWaitHeaders, SideUpstream, MilestoneHeaders, false, "headers")
	ev := tr.Events()[0]
	if ev.Attempt != 2 {
		t.Fatalf("attempt=%d", ev.Attempt)
	}
}

func TestStoreRoundTripsAttempt(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 8)
	tr := New("req-persist-attempt", 8)
	tr.SetAttempt(3)
	tr.Mark(StageUpstreamWaitHeaders, SideUpstream, MilestoneHeaders, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("req-persist-attempt")
	if err != nil {
		t.Fatal(err)
	}
	events := got.Events()
	if len(events) != 1 || events[0].Attempt != 3 {
		t.Fatalf("events=%#v", events)
	}
}
