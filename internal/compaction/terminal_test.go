package compaction

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestClassifyStopNormalizesProviderVocabularies(t *testing.T) {
	cases := map[string]Class{
		"":                              ClassCompleted,
		"stop":                          ClassCompleted,
		"length":                        ClassIncomplete,
		"max_tokens":                    ClassIncomplete,
		"content-filter":                ClassIncomplete,
		"model_context_window_exceeded": ClassIncomplete,
		"pause_turn":                    ClassIncomplete,
		"refusal":                       ClassIncomplete,
		"error":                         ClassFailed,
		"unknown-ordinary":              ClassCompleted,
	}
	for reason, want := range cases {
		if got := ClassifyStop(reason); got != want {
			t.Fatalf("reason %q: got %s want %s", reason, got, want)
		}
	}
}

func TestReplacementAllowedOnlyAfterTrueCompletedTerminal(t *testing.T) {
	if !ReplacementAllowed(ClassCompleted) {
		t.Fatal("completed should allow replacement")
	}
	if ReplacementAllowed(ClassIncomplete) || ReplacementAllowed(ClassFailed) {
		t.Fatal("incomplete/failed must not install replacement history")
	}
	if ClassifyEvent(protocol.Event{Type: protocol.EventDone}, true) != ClassIncomplete {
		t.Fatal("open tool at done is incomplete")
	}
	if ClassifyEvent(protocol.Event{Type: protocol.EventError, Message: "boom"}, false) != ClassFailed {
		t.Fatal("error event")
	}
}

func TestEffectiveBudgetIsLoweringOnly(t *testing.T) {
	if got := EffectiveBudget(100000, 80000, 0); got != 80000 {
		t.Fatalf("maxInput=%d", got)
	}
	if got := EffectiveBudget(100000, 0, 95000); got != 90000 {
		t.Fatalf("safety=%d", got)
	}
	if got := EffectiveBudget(100000, 0, 200000); got != 90000 {
		t.Fatalf("configured must not raise: %d", got)
	}
}
