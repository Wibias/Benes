package lab

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAppendsHashChain(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	first := Event{EventKind: KindRunStarted, RecordedAt: 1, Producer: "benes-lab", ProducerVersion: "capability-v1"}
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	second := Event{
		EventKind: KindVerdictRecorded, RecordedAt: 2, Producer: "benes-lab", ProducerVersion: "capability-v1",
		SubjectID: "openai-apikey/gpt-5.4", EvidenceLayer: "protocol_conformance", SuiteID: "codex", Verdict: "VERIFIED",
	}
	if err := store.Append(second); err != nil {
		t.Fatal(err)
	}
	events, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sequence != 1 || events[1].PrevHash != events[0].EventHash || events[1].PrevHash == "" {
		t.Fatalf("events=%+v", events)
	}
}

func TestStoreAllReportsCorruptLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewStore(dir).All()
	if err == nil {
		t.Fatal("expected corrupt")
	}
}
