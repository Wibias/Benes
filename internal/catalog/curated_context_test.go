package catalog

import "testing"

func TestEffectiveContextWindowCuratedPrecedenceAndEvidence(t *testing.T) {
	evidence := &ContextEvidence{Source: "models.dev", Revision: "rev", Path: "model.toml"}
	got := EffectiveContextWindow(ContextInput{
		Curated:         1_000_000,
		CuratedEvidence: evidence,
		Static:          262_144,
	})
	if got.Tokens != 1_000_000 || got.Source != ContextCuratedMetadata || got.Evidence == nil || got.Evidence.Path != "model.toml" {
		t.Fatalf("curated=%#v", got)
	}

	discovered := EffectiveContextWindow(ContextInput{
		Discovered:      900_000,
		Curated:         1_000_000,
		CuratedEvidence: evidence,
	})
	if discovered.Tokens != 900_000 || discovered.Source != ContextDiscovered || discovered.Evidence != nil {
		t.Fatalf("discovered=%#v", discovered)
	}

	operator := EffectiveContextWindow(ContextInput{
		Operator:        700_000,
		OperatorMode:    WindowOverride,
		Discovered:      900_000,
		Curated:         1_000_000,
		CuratedEvidence: evidence,
	})
	if operator.Tokens != 700_000 || operator.Source != ContextOperator || operator.Evidence != nil {
		t.Fatalf("operator=%#v", operator)
	}

	capped := EffectiveContextWindow(ContextInput{
		Curated:         1_000_000,
		CuratedEvidence: evidence,
		Cap:             500_000,
	})
	if capped.Tokens != 500_000 || capped.Source != ContextCap || capped.Evidence != nil {
		t.Fatalf("capped=%#v", capped)
	}
}

func TestCuratedContextForIsProviderScopedAndPinned(t *testing.T) {
	got, maxOutput, ok := CuratedContextFor("opencode-go", "qwen3.8-max")
	if !ok {
		t.Fatal("missing qwen3.8-max curated context")
	}
	if got.Tokens != 1_000_000 || maxOutput != 131_072 || got.Source != ContextCuratedMetadata {
		t.Fatalf("got=%#v maxOutput=%d", got, maxOutput)
	}
	if got.Evidence == nil ||
		got.Evidence.Source != "models.dev" ||
		got.Evidence.Revision != "dff014f67f04d7f17b6a7024510c77dd4a5b47e5" ||
		got.Evidence.Path != "models/alibaba/qwen3.8-max.toml" {
		t.Fatalf("evidence=%#v", got.Evidence)
	}
	if _, _, ok := CuratedContextFor("other", "qwen3.8-max"); ok {
		t.Fatal("curated context crossed provider boundary")
	}
	if _, _, ok := CuratedContextFor("opencode-go", "unknown-model"); ok {
		t.Fatal("unknown model received curated context")
	}
}

func TestCuratedOpenCodeGoSnapshotHasNoDuplicateOrInvalidRows(t *testing.T) {
	if len(curatedContextRows) != 36 {
		t.Fatalf("rows=%d want=36", len(curatedContextRows))
	}
	for key, row := range curatedContextRows {
		if row.Context <= 0 || row.Path == "" {
			t.Fatalf("%s=%#v", key, row)
		}
	}
}
