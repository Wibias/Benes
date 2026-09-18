package usage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLookupExactGatewayHitAndNearMiss(t *testing.T) {
	table := DefaultTable()
	table.AddAlias(Alias{
		GatewayProvider:      "openrouter",
		AliasModel:           "x-ai/grok-4.6",
		ManufacturerProvider: "xai",
		ManufacturerModel:    "grok-4.6",
		LowerBound:           true,
	})

	hit, ok := table.Lookup("openrouter", "x-ai/grok-4.6", 0)
	if !ok || hit.Confidence != ConfidenceLowerBound || hit.Input != 2 || hit.SourceClass != SourceGateway {
		t.Fatalf("alias hit=%v %#v", ok, hit)
	}
	if _, miss := table.Lookup("openrouter", "x-ai/grok-4.6-preview", 0); miss {
		t.Fatal("near-miss inherited manufacturer rate")
	}
	if _, miss := table.Lookup("openrouter", "grok-4.6", 0); miss {
		t.Fatal("bare grok-4.6 on gateway inherited xAI rate")
	}
}

func TestSameSlugOnTwoGatewaysStaysIndependent(t *testing.T) {
	table := DefaultTable()
	table.AddAlias(Alias{
		GatewayProvider:      "openrouter",
		AliasModel:           "grok-4.6",
		ManufacturerProvider: "xai",
		ManufacturerModel:    "grok-4.6",
		LowerBound:           true,
	})
	or, orOK := table.Lookup("openrouter", "grok-4.6", 0)
	together, togetherOK := table.Lookup("together", "grok-4.6", 0)
	if !orOK || togetherOK {
		t.Fatalf("openrouter=%v together=%v", orOK, togetherOK)
	}
	if or.Confidence != ConfidenceLowerBound {
		t.Fatalf("openrouter confidence=%s", or.Confidence)
	}
	_ = together
}

func TestSourcedZeroIsPricedAndUnsourcedFreeAliasIsUnknown(t *testing.T) {
	table := NewTable()
	table.Add(PriceRecord{
		Provider:    "gateway",
		Model:       "promo-zero",
		SourceClass: SourceGateway,
		SourceRef:   "gateway/promo-2026",
		Status:      StatusVerified,
		Currency:    "USD",
	})
	zero, ok := table.Lookup("gateway", "promo-zero", 0)
	if !ok {
		t.Fatal("sourced zero missing")
	}
	usd, conf := zero.Estimate(TokenUse{Input: 1_000_000}, "", "")
	if usd != 0 || conf != ConfidenceExact {
		t.Fatalf("sourced zero billed=%v conf=%s", usd, conf)
	}
	if _, found := table.Lookup("gateway", "llama-3-free", 0); found {
		t.Fatal("unsourced -free alias was priced")
	}
}

func TestManufacturerFallbackEstimateIsLabeled(t *testing.T) {
	table := DefaultTable()
	table.AddAlias(Alias{
		GatewayProvider:      "openrouter",
		AliasModel:           "x-ai/grok-4.6",
		ManufacturerProvider: "xai",
		ManufacturerModel:    "grok-4.6",
	})
	got, ok := table.Lookup("openrouter", "x-ai/grok-4.6", 0)
	if !ok || got.Confidence != ConfidenceEstimated || got.Status != StatusDerived {
		t.Fatalf("fallback=%v %#v", ok, got)
	}
}

func TestDerivedThirdPartySourceAndDatedFX(t *testing.T) {
	table := NewTable()
	table.Add(PriceRecord{
		Provider:    "xiaomi",
		Model:       "MiMo-V2.5-Pro",
		Cost4:       Cost4{Input: 3, Output: 6},
		SourceClass: SourceDerivedThirdParty,
		SourceRef:   "mimo.mi.com/docs/news/billing",
		Status:      StatusDerived,
		Currency:    "CNY",
		FXRate:      0.25,
		FXDate:      "2026-08-01",
		FXSource:    "operator-export",
	})
	got, ok := table.Lookup("xiaomi", "MiMo-V2.5-Pro", 0)
	if !ok || got.Confidence != ConfidenceEstimated {
		t.Fatalf("fx=%v %#v", ok, got)
	}
	usd, _ := got.Estimate(TokenUse{Input: 1_000_000}, "", "")
	if usd != 0.75 {
		t.Fatalf("usd=%v rates=%#v", usd, got.Cost4)
	}

	unknownFX := NewTable()
	unknownFX.Add(PriceRecord{
		Provider:    "xiaomi",
		Model:       "MiMo-V2.5-Pro",
		Cost4:       Cost4{Input: 3, Output: 6},
		SourceClass: SourceDerivedThirdParty,
		Status:      StatusDerived,
		Currency:    "CNY",
	})
	if _, ok := unknownFX.Lookup("xiaomi", "MiMo-V2.5-Pro", 0); ok {
		t.Fatal("undated FX converted with an inline constant")
	}
}

func TestStaleRowAndOperatorOverride(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{
		Provider:      "openai-apikey",
		Model:         "gpt-5.5",
		Cost4:         Cost4{Input: 5, Output: 15},
		SourceClass:   SourceManufacturer,
		Status:        StatusVerified,
		Currency:      "USD",
		VerifiedUntil: now - 1,
	})
	stale, ok := table.Lookup("openai-apikey", "gpt-5.5", now)
	if !ok || stale.Confidence != ConfidenceStale || stale.Status != StatusStale {
		t.Fatalf("stale=%v %#v", ok, stale)
	}
	table.Add(PriceRecord{
		Provider:    "openai-apikey",
		Model:       "gpt-5.5",
		Cost4:       Cost4{Input: 1, Output: 2},
		SourceClass: SourceOperator,
		Status:      StatusOperator,
		Currency:    "USD",
	})
	over, ok := table.Lookup("openai-apikey", "gpt-5.5", now)
	if !ok || over.Confidence != ConfidenceExact || over.Input != 1 || over.SourceClass != SourceOperator {
		t.Fatalf("operator=%v %#v", ok, over)
	}
	if bundled, _ := table.records[priceKey("openai-apikey", "gpt-5.5")]; bundled.Input != 5 {
		t.Fatalf("operator mutated bundled record: %#v", bundled)
	}
}

func TestConfirmedTierAndLowerBoundPriorityStayProviderScoped(t *testing.T) {
	table := DefaultTable()
	canonical, ok := table.Lookup("xai", "grok-4.6", 0)
	if !ok {
		t.Fatal("missing bundled grok-4.6")
	}
	usd, conf := canonical.Estimate(TokenUse{Input: 1000}, "priority", "priority")
	if conf != ConfidenceExact || usd <= 0 {
		t.Fatalf("confirmed=%s usd=%v", conf, usd)
	}
	long, longConf := canonical.Estimate(TokenUse{Input: 200_000}, "priority", "priority")
	if longConf != ConfidenceLowerBound {
		t.Fatalf("long-context combined invented rate: %s usd=%v", longConf, long)
	}
	table.AddAlias(Alias{
		GatewayProvider:      "openrouter",
		AliasModel:           "grok-4.6",
		ManufacturerProvider: "xai",
		ManufacturerModel:    "grok-4.6",
	})
	reseller, _ := table.Lookup("openrouter", "grok-4.6", 0)
	_, resellerConf := reseller.Estimate(TokenUse{Input: 1000}, "priority", "priority")
	if resellerConf != ConfidenceEstimated {
		t.Fatalf("reseller inherited exact xAI tier: conf=%s %#v", resellerConf, reseller)
	}
}

func TestOverlaysFromDiskProvidersSkipSecrets(t *testing.T) {
	raw := json.RawMessage(`{"adapter":"openai-responses","modelCosts":{"gpt-5.5":{"input":1,"output":2},"sk-abcdef":{"input":9,"output":9}}}`)
	got := OverlaysFromDiskProviders(map[string]json.RawMessage{"openai-apikey": raw})
	if len(got) != 1 || got[0].Model != "gpt-5.5" || got[0].Input != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestCacheTokensAreNotDoubleBilled(t *testing.T) {
	table := NewTable()
	table.Add(PriceRecord{
		Provider:    "openai-apikey",
		Model:       "gpt-5.5",
		Cost4:       Cost4{Input: 5, Output: 15, CacheRead: 0.5, CacheWrite: 2.5},
		SourceClass: SourceManufacturer,
		Status:      StatusVerified,
		Currency:    "USD",
	})
	got, _ := table.Lookup("openai-apikey", "gpt-5.5", 0)
	usd, _ := got.Estimate(TokenUse{Input: 100, Output: 0, CacheRead: 20, CacheWrite: 20}, "", "")
	want := (60*5 + 20*0.5 + 20*2.5) / 1_000_000
	if usd != want {
		t.Fatalf("usd=%v want=%v", usd, want)
	}
}
