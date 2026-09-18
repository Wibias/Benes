package usage

import (
	"encoding/json"
	"strings"
)

type SourceClass string

const (
	SourceManufacturer      SourceClass = "manufacturer"
	SourceGateway           SourceClass = "gateway"
	SourceDerivedThirdParty SourceClass = "derived-third-party"
	SourceOperator          SourceClass = "operator"
)

type PriceStatus string

const (
	StatusVerified PriceStatus = "verified"
	StatusDerived  PriceStatus = "derived"
	StatusOperator PriceStatus = "operator"
	StatusStale    PriceStatus = "stale"
	StatusUnknown  PriceStatus = "unknown"
)

type Confidence string

const (
	ConfidenceExact      Confidence = "exact"
	ConfidenceEstimated  Confidence = "estimated"
	ConfidenceLowerBound Confidence = "lower_bound"
	ConfidenceStale      Confidence = "stale"
	ConfidenceUnknown    Confidence = "unknown"
)

type PriceRecord struct {
	Provider    string
	Destination string
	Model       string
	Cost4
	SourceClass   SourceClass
	SourceRef     string
	VerifiedAt    int64
	VerifiedUntil int64
	Currency      string
	FXRate        float64
	FXDate        string
	FXSource      string
	Status        PriceStatus
	LowerBound    bool
}

type Alias struct {
	GatewayProvider      string
	AliasModel           string
	ManufacturerProvider string
	ManufacturerModel    string
	LowerBound           bool
}

type TokenUse struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

type ResolvedPrice struct {
	PriceRecord
	Confidence Confidence
	USD        float64
}

type Table struct {
	records  map[string]PriceRecord
	aliases  map[string]Alias
	operator map[string]PriceRecord
}

func priceKey(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "\x00" + strings.TrimSpace(model)
}

func NewTable() *Table {
	return &Table{
		records:  map[string]PriceRecord{},
		aliases:  map[string]Alias{},
		operator: map[string]PriceRecord{},
	}
}

func DefaultTable() *Table {
	table := NewTable()
	table.Add(PriceRecord{
		Provider:    "xai",
		Model:       "grok-4.6",
		Cost4:       Grok46VerifiedBase,
		SourceClass: SourceManufacturer,
		SourceRef:   "x.ai/docs/models#grok-4.6",
		Status:      StatusVerified,
		Currency:    "USD",
	})
	return table
}

func (t *Table) Add(record PriceRecord) {
	if t == nil || strings.TrimSpace(record.Provider) == "" || strings.TrimSpace(record.Model) == "" {
		return
	}
	if record.Currency == "" {
		record.Currency = "USD"
	}
	key := priceKey(record.Provider, record.Model)
	if record.SourceClass == SourceOperator || record.Status == StatusOperator {
		record.SourceClass = SourceOperator
		record.Status = StatusOperator
		t.operator[key] = record
		return
	}
	t.records[key] = record
}

func (t *Table) AddAlias(alias Alias) {
	if t == nil || strings.TrimSpace(alias.GatewayProvider) == "" || strings.TrimSpace(alias.AliasModel) == "" {
		return
	}
	t.aliases[priceKey(alias.GatewayProvider, alias.AliasModel)] = alias
}

func (t *Table) AddOperatorOverlays(records []PriceRecord) {
	for _, record := range records {
		record.SourceClass = SourceOperator
		record.Status = StatusOperator
		t.Add(record)
	}
}

func (t *Table) Lookup(provider, model string, now int64) (ResolvedPrice, bool) {
	if t == nil {
		return ResolvedPrice{}, false
	}
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return ResolvedPrice{}, false
	}
	key := priceKey(provider, model)
	if record, ok := t.operator[key]; ok {
		return finalize(record, ConfidenceExact, now)
	}
	if record, ok := t.records[key]; ok {
		confidence := ConfidenceExact
		if record.LowerBound {
			confidence = ConfidenceLowerBound
		} else if record.Status == StatusDerived || record.SourceClass == SourceDerivedThirdParty {
			confidence = ConfidenceEstimated
		}
		return finalize(record, confidence, now)
	}
	alias, ok := t.aliases[key]
	if !ok {
		return ResolvedPrice{}, false
	}
	source, ok := t.records[priceKey(alias.ManufacturerProvider, alias.ManufacturerModel)]
	if !ok {
		return ResolvedPrice{}, false
	}
	mapped := source
	mapped.Provider = provider
	mapped.Model = model
	mapped.SourceClass = SourceGateway
	mapped.SourceRef = alias.ManufacturerProvider + "/" + alias.ManufacturerModel
	mapped.Status = StatusDerived
	mapped.LowerBound = mapped.LowerBound || alias.LowerBound
	confidence := ConfidenceEstimated
	if mapped.LowerBound {
		confidence = ConfidenceLowerBound
	}
	return finalize(mapped, confidence, now)
}

func finalize(record PriceRecord, confidence Confidence, now int64) (ResolvedPrice, bool) {
	if record.VerifiedUntil > 0 && now > 0 && now > record.VerifiedUntil {
		record.Status = StatusStale
		confidence = ConfidenceStale
	}
	usdRates, ok := usdCost(record)
	if !ok {
		return ResolvedPrice{}, false
	}
	record.Cost4 = usdRates
	record.Currency = "USD"
	return ResolvedPrice{PriceRecord: record, Confidence: confidence}, true
}

func usdCost(record PriceRecord) (Cost4, bool) {
	currency := strings.ToUpper(strings.TrimSpace(record.Currency))
	if currency == "" || currency == "USD" {
		return record.Cost4, true
	}
	if record.FXRate <= 0 || strings.TrimSpace(record.FXDate) == "" || strings.TrimSpace(record.FXSource) == "" {
		return Cost4{}, false
	}
	return Cost4{
		Input:      record.Input * record.FXRate,
		Output:     record.Output * record.FXRate,
		CacheRead:  record.CacheRead * record.FXRate,
		CacheWrite: record.CacheWrite * record.FXRate,
	}, true
}

func (r ResolvedPrice) Estimate(tokens TokenUse, confirmedTier, requestedTier string) (float64, Confidence) {
	rates := r.Cost4
	confidence := r.Confidence
	if r.SourceClass != SourceOperator && strings.EqualFold(r.Provider, "xai") && r.Model == "grok-4.6" {
		estimate := EstimateXAI(r.Provider, r.Model, confirmedTier, requestedTier, tokens.Input, rates, Grok46PriorityMultiplier)
		rates = estimate.Cost4
		if estimate.LowerBound {
			confidence = ConfidenceLowerBound
		}
	}
	billableInput := tokens.Input - tokens.CacheRead - tokens.CacheWrite
	if billableInput < 0 {
		billableInput = 0
	}
	usd := (float64(billableInput)*rates.Input +
		float64(tokens.Output)*rates.Output +
		float64(tokens.CacheRead)*rates.CacheRead +
		float64(tokens.CacheWrite)*rates.CacheWrite) / 1_000_000
	return usd, confidence
}

func OverlaysFromDiskProviders(providers map[string]json.RawMessage) []PriceRecord {
	if len(providers) == 0 {
		return nil
	}
	out := make([]PriceRecord, 0)
	for id, raw := range providers {
		var provider map[string]json.RawMessage
		if json.Unmarshal(raw, &provider) != nil || provider == nil {
			continue
		}
		costsRaw, ok := provider["modelCosts"]
		if !ok {
			continue
		}
		var costs map[string]Cost4JSON
		if json.Unmarshal(costsRaw, &costs) != nil {
			continue
		}
		for model, cost := range costs {
			model = strings.TrimSpace(model)
			if model == "" || strings.HasPrefix(strings.ToLower(model), "sk-") {
				continue
			}
			out = append(out, PriceRecord{
				Provider:    id,
				Model:       model,
				Cost4:       cost.Cost4(),
				SourceClass: SourceOperator,
				SourceRef:   "config.modelCosts",
				Status:      StatusOperator,
				Currency:    "USD",
			})
		}
	}
	return out
}

type Cost4JSON struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

func (c Cost4JSON) Cost4() Cost4 {
	return Cost4{Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite: c.CacheWrite}
}

func tokensFrom(item entry) TokenUse {
	if item.Usage == nil {
		return TokenUse{}
	}
	read, write := cacheTokens(item)
	return TokenUse{
		Input:      item.Usage.InputTokens,
		Output:     item.Usage.OutputTokens,
		CacheRead:  read,
		CacheWrite: write,
	}
}

func cacheTokens(item entry) (read, write int64) {
	if item.Usage == nil {
		return 0, 0
	}
	if item.Usage.CacheCreationInputTokens != nil {
		write = *item.Usage.CacheCreationInputTokens
	}
	switch {
	case item.Usage.CacheReadInputTokens != nil:
		read = *item.Usage.CacheReadInputTokens
	case item.Usage.CachedInputTokens != nil && item.Usage.CacheCreationInputTokens != nil:
		read = *item.Usage.CachedInputTokens - *item.Usage.CacheCreationInputTokens
		if read < 0 {
			read = 0
		}
	case item.Usage.CachedInputTokens != nil:
		read = *item.Usage.CachedInputTokens
	}
	return read, write
}

func (t *Table) Price(item entry, now int64) (ResolvedPrice, bool) {
	resolved, ok := t.Lookup(item.Provider, item.Model, now)
	if !ok {
		return ResolvedPrice{}, false
	}
	usd, confidence := resolved.Estimate(tokensFrom(item), "", "")
	resolved.USD = usd
	resolved.Confidence = confidence
	return resolved, true
}
