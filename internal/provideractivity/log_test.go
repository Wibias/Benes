package provideractivity

import "testing"

func TestLogDropsOldestWhenFull(t *testing.T) {
	log := New()
	for i := 0; i < maxEvents+3; i++ {
		log.Record(Event{Provider: "openai", Type: "tick", Timestamp: int64(i + 1)})
	}
	got := log.Recent(maxEvents)
	if len(got) != maxEvents {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Timestamp != 4 || got[len(got)-1].Timestamp != int64(maxEvents+3) {
		t.Fatalf("range=%d..%d", got[0].Timestamp, got[len(got)-1].Timestamp)
	}
}

func TestLogFiltersByProvider(t *testing.T) {
	log := New()
	log.Record(Event{Provider: "openai", Type: "a", Timestamp: 1})
	log.Record(Event{Provider: "anthropic", Type: "b", Timestamp: 2})
	log.Record(Event{Provider: "openai", Type: "c", Timestamp: 3})
	got := log.RecentFor("openai", 10)
	if len(got) != 2 || got[0].Type != "a" || got[1].Type != "c" {
		t.Fatalf("got=%#v", got)
	}
}
