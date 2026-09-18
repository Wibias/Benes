package sessions

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAggregatesCoverExactUsageWithoutTreatingMissingAsZero(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "agg-usage"}
	first := sampleRecord("req-full", identity, time.UnixMilli(1000), int64Ptr(10))
	cost := 1.25
	first.Usage.Cost = &cost
	first.Usage.Currency = "USD"
	first.Routing = Routing{ResolvedModel: "gpt-5.6", Provider: "openai-apikey"}
	id, err := store.Record(first)
	if err != nil || id == "" {
		t.Fatalf("first=%q err=%v", id, err)
	}
	partial := sampleRecord("req-partial", identity, time.UnixMilli(2000), nil)
	partial.Routing = Routing{ResolvedModel: "gpt-5.6", Provider: "openai-apikey"}
	if _, err := store.Record(partial); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Requests) != 1 || !detail.HasMore {
		t.Fatalf("page should not change aggregates: %#v", detail.Requests)
	}
	usage := detail.Aggregates.Usage
	if usage.InputTokens.Value == nil || *usage.InputTokens.Value != 10 {
		t.Fatalf("input=%#v", usage.InputTokens)
	}
	if usage.InputTokens.AttributedRequests != 1 || usage.InputTokens.TotalRequests != 2 || usage.InputTokens.Complete {
		t.Fatalf("input coverage=%#v", usage.InputTokens)
	}
	if usage.Cost.Value == nil || *usage.Cost.Value != 1.25 || usage.Cost.Currency != "USD" || usage.Cost.Complete {
		t.Fatalf("cost=%#v", usage.Cost)
	}
	fullPage, err := store.Get(id, DetailOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if fullPage.Aggregates.Usage.InputTokens.AttributedRequests != usage.InputTokens.AttributedRequests {
		t.Fatalf("limit changed aggregates")
	}
}

func TestAggregatesCompleteWhenEveryRequestHasExactUsage(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "agg-complete"}
	first := sampleRecord("req-a", identity, time.UnixMilli(1000), int64Ptr(4))
	cost := 0.5
	first.Usage.Cost = &cost
	first.Usage.Currency = "USD"
	id, err := store.Record(first)
	if err != nil {
		t.Fatal(err)
	}
	second := sampleRecord("req-b", identity, time.UnixMilli(2000), int64Ptr(6))
	second.Usage.Cost = &cost
	second.Usage.Currency = "USD"
	if _, err := store.Record(second); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Aggregates.Usage.InputTokens.Complete || *detail.Aggregates.Usage.InputTokens.Value != 10 {
		t.Fatalf("input=%#v", detail.Aggregates.Usage.InputTokens)
	}
	if !detail.Aggregates.Usage.Cost.Complete || *detail.Aggregates.Usage.Cost.Value != 1.0 {
		t.Fatalf("cost=%#v", detail.Aggregates.Usage.Cost)
	}
}

func TestAggregatesSkipMalformedNumericRows(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "agg-corrupt"}
	first := sampleRecord("req-ok", identity, time.UnixMilli(1000), int64Ptr(8))
	cost := 2.0
	first.Usage.Cost = &cost
	first.Usage.Currency = "USD"
	id, err := store.Record(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(sampleRecord("req-bad", identity, time.UnixMilli(2000), int64Ptr(99))); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, err = store.db.Exec(`UPDATE requests SET input_tokens = 'nope', cost = 'nope' WHERE id = 'req-bad'`)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Aggregates.Usage.InputTokens.Value == nil || *detail.Aggregates.Usage.InputTokens.Value != 8 {
		t.Fatalf("corrupt row leaked into total %#v", detail.Aggregates.Usage.InputTokens)
	}
	if detail.Aggregates.Usage.InputTokens.Complete {
		t.Fatal("malformed skip must mark usage incomplete")
	}
}

func TestAggregatesOmitMixedCurrencyTotals(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "agg-fx"}
	usd := sampleRecord("req-usd", identity, time.UnixMilli(1000), int64Ptr(1))
	usdCost := 1.0
	usd.Usage.Cost = &usdCost
	usd.Usage.Currency = "USD"
	id, err := store.Record(usd)
	if err != nil {
		t.Fatal(err)
	}
	eur := sampleRecord("req-eur", identity, time.UnixMilli(2000), int64Ptr(1))
	eurCost := 2.0
	eur.Usage.Cost = &eurCost
	eur.Usage.Currency = "EUR"
	if _, err := store.Record(eur); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Aggregates.Usage.Cost.Value != nil || detail.Aggregates.Usage.Cost.Complete {
		t.Fatalf("mixed currencies must not sum %#v", detail.Aggregates.Usage.Cost)
	}
	if !containsAll(detail.Aggregates.Usage.Cost.Currencies, "USD", "EUR") {
		t.Fatalf("currencies=%v", detail.Aggregates.Usage.Cost.Currencies)
	}
}

func TestAggregatesRoutingAndFailover(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "agg-route"}
	direct := sampleRecord("req-direct", identity, time.UnixMilli(1000), nil)
	direct.Routing = Routing{Kind: "direct", ResolvedModel: "gpt-5.6", Provider: "openai-apikey"}
	id, err := store.Record(direct)
	if err != nil {
		t.Fatal(err)
	}
	comboOK := sampleRecord("req-combo-ok", identity, time.UnixMilli(2000), nil)
	comboOK.Routing = Routing{Kind: "combo", ComboID: "fast", ResolvedModel: "gpt-5.4", Provider: "openai-apikey", CommittedMember: "openai-apikey/gpt-5.4"}
	comboOK.Attempts = []Attempt{{Member: "openai-apikey/gpt-5.4", Status: 200, Decision: "committed"}}
	if _, err := store.Record(comboOK); err != nil {
		t.Fatal(err)
	}
	comboHop := sampleRecord("req-combo-hop", identity, time.UnixMilli(3000), nil)
	comboHop.Routing = Routing{Kind: "combo", ComboID: "fast", ResolvedModel: "claude-sonnet", Provider: "anthropic", CommittedMember: "anthropic/claude-sonnet"}
	comboHop.Attempts = []Attempt{
		{Member: "openai-apikey/gpt-5.4", Status: 503, Decision: "hop"},
		{Member: "anthropic/claude-sonnet", Status: 200, Decision: "committed"},
	}
	if _, err := store.Record(comboHop); err != nil {
		t.Fatal(err)
	}
	policy := sampleRecord("req-policy", identity, time.UnixMilli(4000), nil)
	policy.Protocol = ProtocolChat
	policy.Path = "/v1/chat/completions"
	policy.Routing = Routing{Kind: "policy", PolicyID: "cheap", ResolvedModel: "gpt-5.6", Provider: "openai-apikey"}
	if _, err := store.Record(policy); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	agg := detail.Aggregates
	if !containsAll(agg.Models, "gpt-5.6", "gpt-5.4", "claude-sonnet") {
		t.Fatalf("models=%v", agg.Models)
	}
	if !containsAll(agg.Providers, "openai-apikey", "anthropic") {
		t.Fatalf("providers=%v", agg.Providers)
	}
	if !containsAll(agg.ComboIDs, "fast") || !containsAll(agg.PolicyIDs, "cheap") {
		t.Fatalf("combo=%v policy=%v", agg.ComboIDs, agg.PolicyIDs)
	}
	if !containsAll(agg.Protocols, ProtocolResponses, ProtocolChat) {
		t.Fatalf("protocols=%v", agg.Protocols)
	}
	if agg.FailoverRequestCount != 1 || !agg.HadFailover {
		t.Fatalf("failover=%d had=%v", agg.FailoverRequestCount, agg.HadFailover)
	}
}

func TestAggregatesSurviveStoreReopen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sessions.sqlite"
	store, err := open(path, func() time.Time { return time.UnixMilli(10_000_000) })
	if err != nil {
		t.Fatal(err)
	}
	in := sampleRecord("req-1", Identity{Namespace: NamespaceCodexThread, ExternalID: "persist"}, time.UnixMilli(1000), int64Ptr(3))
	in.Routing = Routing{ResolvedModel: "gpt-5.6", Provider: "openai-apikey"}
	id, err := store.Record(in)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := open(path, func() time.Time { return time.UnixMilli(10_000_000) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	second, err := reopened.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *second.Aggregates.Usage.InputTokens.Value != *first.Aggregates.Usage.InputTokens.Value {
		t.Fatalf("usage changed after reopen")
	}
	if strings.Join(second.Aggregates.Models, ",") != strings.Join(first.Aggregates.Models, ",") {
		t.Fatalf("models changed after reopen")
	}
}

func TestListSearchAndFilters(t *testing.T) {
	store := openStore(t)
	codex := sampleRecord("req-codex", Identity{Namespace: NamespaceCodexThread, ExternalID: "thread-search"}, time.UnixMilli(3000), nil)
	codex.Routing = Routing{ResolvedModel: "gpt-5.6", Provider: "openai-apikey", PolicyID: "cheap"}
	codexID, err := store.Record(codex)
	if err != nil {
		t.Fatal(err)
	}
	chat := sampleRecord("req-chat", Identity{Namespace: "chat_completions/session", ExternalID: "other-id"}, time.UnixMilli(2000), nil)
	chat.Protocol = ProtocolChat
	chat.Path = "/v1/chat/completions"
	chat.Routing = Routing{ResolvedModel: "claude-sonnet", Provider: "anthropic", ComboID: "fast"}
	chatID, err := store.Record(chat)
	if err != nil {
		t.Fatal(err)
	}
	byID, err := store.List(ListOptions{Q: strings.ToUpper(codexID)})
	if err != nil || len(byID.Sessions) != 1 || byID.Sessions[0].ID != codexID {
		t.Fatalf("id search=%#v err=%v", byID, err)
	}
	byExternal, err := store.List(ListOptions{Q: "THREAD-search"})
	if err != nil || len(byExternal.Sessions) != 1 || byExternal.Sessions[0].ID != codexID {
		t.Fatalf("external search=%#v err=%v", byExternal, err)
	}
	byNS, err := store.List(ListOptions{Q: "chat_completions/session"})
	if err != nil || len(byNS.Sessions) != 1 || byNS.Sessions[0].ID != chatID {
		t.Fatalf("namespace search=%#v err=%v", byNS, err)
	}
	blank, err := store.List(ListOptions{Q: "   "})
	if err != nil || len(blank.Sessions) != 2 {
		t.Fatalf("blank q should be unfiltered %#v err=%v", blank, err)
	}
	protocol, err := store.List(ListOptions{Protocol: ProtocolChat})
	if err != nil || len(protocol.Sessions) != 1 || protocol.Sessions[0].ID != chatID {
		t.Fatalf("protocol=%#v err=%v", protocol, err)
	}
	namespace, err := store.List(ListOptions{Namespace: NamespaceCodexThread})
	if err != nil || len(namespace.Sessions) != 1 || namespace.Sessions[0].ID != codexID {
		t.Fatalf("namespace filter=%#v err=%v", namespace, err)
	}
	combined, err := store.List(ListOptions{Q: "thread", Protocol: ProtocolResponses, Namespace: NamespaceCodexThread})
	if err != nil || len(combined.Sessions) != 1 || combined.Sessions[0].ID != codexID {
		t.Fatalf("combined=%#v err=%v", combined, err)
	}
	provider, err := store.List(ListOptions{Provider: "anthropic", Model: "claude-sonnet", Combo: "fast"})
	if err != nil || len(provider.Sessions) != 1 || provider.Sessions[0].ID != chatID {
		t.Fatalf("provider/model/combo=%#v err=%v", provider, err)
	}
	policy, err := store.List(ListOptions{Policy: "cheap"})
	if err != nil || len(policy.Sessions) != 1 || policy.Sessions[0].ID != codexID {
		t.Fatalf("policy=%#v err=%v", policy, err)
	}
	if _, err := store.List(ListOptions{Protocol: "nope"}); err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("unknown protocol err=%v", err)
	}
	if _, err := store.List(ListOptions{Q: strings.Repeat("x", maxSearchQuery+1)}); err != ErrInvalidQuery {
		t.Fatalf("long q err=%v", err)
	}
}

func TestListFilteredPaginationDoesNotLeak(t *testing.T) {
	store := openStore(t)
	for i := 1; i <= 3; i++ {
		in := sampleRecord("req-keep-"+strconv.Itoa(i), Identity{Namespace: NamespaceCodexThread, ExternalID: "keep-" + strconv.Itoa(i)}, time.UnixMilli(int64(1000*i)), nil)
		in.Protocol = ProtocolResponses
		if _, err := store.Record(in); err != nil {
			t.Fatal(err)
		}
	}
	other := sampleRecord("req-other", Identity{Namespace: "chat_completions/session", ExternalID: "other"}, time.UnixMilli(4000), nil)
	other.Protocol = ProtocolChat
	if _, err := store.Record(other); err != nil {
		t.Fatal(err)
	}
	page1, err := store.List(ListOptions{Limit: 2, Protocol: ProtocolResponses})
	if err != nil || !page1.HasMore || len(page1.Sessions) != 2 {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	for _, row := range page1.Sessions {
		if row.Namespace != NamespaceCodexThread {
			t.Fatalf("leaked %s", row.ID)
		}
	}
	page2, err := store.List(ListOptions{Limit: 2, Protocol: ProtocolResponses, Cursor: page1.NextCursor})
	if err != nil || page2.HasMore || len(page2.Sessions) != 1 {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	if page2.Sessions[0].Namespace != NamespaceCodexThread {
		t.Fatalf("page2 leaked %#v", page2.Sessions[0])
	}
}

func TestExpiredSessionsExcludedFromSearchFiltersAndMetadata(t *testing.T) {
	clock := &testClock{t: time.UnixMilli(10_000_000)}
	store, err := open(t.TempDir()+"/sessions.sqlite", clock.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	old := sampleRecord("old", Identity{Namespace: NamespaceCodexThread, ExternalID: "expired"}, clock.now(), nil)
	old.Routing = Routing{ResolvedModel: "old-model", Provider: "old-provider"}
	oldID, err := store.Record(old)
	if err != nil {
		t.Fatal(err)
	}
	clock.set(clock.now().Add(31 * 24 * time.Hour))
	live := sampleRecord("live", Identity{Namespace: "chat_completions/session", ExternalID: "live"}, clock.now(), nil)
	live.Protocol = ProtocolChat
	live.Routing = Routing{ResolvedModel: "live-model", Provider: "live-provider"}
	liveID, err := store.Record(live)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ListOptions{Q: "expired"})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("expired search %#v err=%v", listed, err)
	}
	if _, err := store.Get(oldID, DetailOptions{}); err != ErrNotFound {
		t.Fatalf("expired get err=%v", err)
	}
	listed, err = store.List(ListOptions{Protocol: ProtocolChat})
	if err != nil || len(listed.Sessions) != 1 || listed.Sessions[0].ID != liveID {
		t.Fatalf("live filter %#v err=%v", listed, err)
	}
	values, err := store.FilterValues()
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(values.Models, "old-model") || containsAll(values.Namespaces, NamespaceCodexThread) {
		t.Fatalf("expired metadata leaked %#v", values)
	}
	if !containsAll(values.Models, "live-model") || !containsAll(values.Protocols, ProtocolChat) {
		t.Fatalf("live metadata missing %#v", values)
	}
}

func TestFilterValuesAreDeterministic(t *testing.T) {
	store := openStore(t)
	a := sampleRecord("a", Identity{Namespace: NamespaceCodexThread, ExternalID: "a"}, time.UnixMilli(1), nil)
	a.Routing = Routing{ResolvedModel: "z-model", Provider: "b-provider", PolicyID: "p2", ComboID: "c2"}
	if _, err := store.Record(a); err != nil {
		t.Fatal(err)
	}
	b := sampleRecord("b", Identity{Namespace: "chat_completions/session", ExternalID: "b"}, time.UnixMilli(2), nil)
	b.Protocol = ProtocolChat
	b.Routing = Routing{ResolvedModel: "a-model", Provider: "a-provider", PolicyID: "p1", ComboID: "c1"}
	if _, err := store.Record(b); err != nil {
		t.Fatal(err)
	}
	values, err := store.FilterValues()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(values.Models, ",") != "a-model,z-model" {
		t.Fatalf("models=%v", values.Models)
	}
	if strings.Join(values.Providers, ",") != "a-provider,b-provider" {
		t.Fatalf("providers=%v", values.Providers)
	}
}

func TestLikeMetacharactersDoNotWidenSearch(t *testing.T) {
	store := openStore(t)
	if _, err := store.Record(sampleRecord("req-1", Identity{Namespace: NamespaceCodexThread, ExternalID: "100pct"}, time.UnixMilli(1), nil)); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ListOptions{Q: "%"})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("%% search %#v err=%v", listed, err)
	}
	listed, err = store.List(ListOptions{Q: "100pct"})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("literal search %#v err=%v", listed, err)
	}
}

func TestListSummariesIncludeProtocols(t *testing.T) {
	store := openStore(t)
	one := sampleRecord("req-one", Identity{Namespace: NamespaceCodexThread, ExternalID: "one"}, time.UnixMilli(1000), nil)
	oneID, err := store.Record(one)
	if err != nil {
		t.Fatal(err)
	}
	multi := sampleRecord("req-multi-a", Identity{Namespace: NamespaceCodexThread, ExternalID: "multi"}, time.UnixMilli(2000), nil)
	multiID, err := store.Record(multi)
	if err != nil {
		t.Fatal(err)
	}
	chat := sampleRecord("req-multi-b", Identity{Namespace: NamespaceCodexThread, ExternalID: "multi"}, time.UnixMilli(3000), nil)
	chat.Protocol = ProtocolChat
	chat.Path = "/v1/chat/completions"
	if _, err := store.Record(chat); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Summary{}
	for _, row := range listed.Sessions {
		byID[row.ID] = row
	}
	if got := strings.Join(byID[oneID].Protocols, ","); got != ProtocolResponses {
		t.Fatalf("one protocol=%v", byID[oneID].Protocols)
	}
	if got := strings.Join(byID[multiID].Protocols, ","); got != ProtocolChat+","+ProtocolResponses {
		t.Fatalf("multi protocol=%v", byID[multiID].Protocols)
	}
}

func TestListProtocolSummariesSurvivePaginationAndSearch(t *testing.T) {
	store := openStore(t)
	var ids []string
	for i := 1; i <= 3; i++ {
		in := sampleRecord("req-p-"+strconv.Itoa(i), Identity{Namespace: NamespaceCodexThread, ExternalID: "page-" + strconv.Itoa(i)}, time.UnixMilli(int64(1000*i)), nil)
		id, err := store.Record(in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	other := sampleRecord("req-other-proto", Identity{Namespace: "chat_completions/session", ExternalID: "other"}, time.UnixMilli(4000), nil)
	other.Protocol = ProtocolChat
	other.Path = "/v1/chat/completions"
	if _, err := store.Record(other); err != nil {
		t.Fatal(err)
	}
	page1, err := store.List(ListOptions{Limit: 2, Protocol: ProtocolResponses})
	if err != nil || !page1.HasMore || len(page1.Sessions) != 2 {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	for _, row := range page1.Sessions {
		if strings.Join(row.Protocols, ",") != ProtocolResponses {
			t.Fatalf("page1 protocols=%v", row.Protocols)
		}
	}
	page2, err := store.List(ListOptions{Limit: 2, Protocol: ProtocolResponses, Cursor: page1.NextCursor})
	if err != nil || page2.HasMore || len(page2.Sessions) != 1 {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	if strings.Join(page2.Sessions[0].Protocols, ",") != ProtocolResponses {
		t.Fatalf("page2 protocols=%v", page2.Sessions[0].Protocols)
	}
	search, err := store.List(ListOptions{Q: ids[0]})
	if err != nil || len(search.Sessions) != 1 || strings.Join(search.Sessions[0].Protocols, ",") != ProtocolResponses {
		t.Fatalf("search=%#v err=%v", search, err)
	}
}

func TestListProtocolsIgnoreMalformedProtocolCells(t *testing.T) {
	store := openStore(t)
	in := sampleRecord("req-ok", Identity{Namespace: NamespaceCodexThread, ExternalID: "proto-corrupt"}, time.UnixMilli(1000), nil)
	id, err := store.Record(in)
	if err != nil {
		t.Fatal(err)
	}
	bad := sampleRecord("req-bad-proto", Identity{Namespace: NamespaceCodexThread, ExternalID: "proto-corrupt"}, time.UnixMilli(2000), nil)
	if _, err := store.Record(bad); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET protocol = 123 WHERE id = 'req-bad-proto'`)
	listed, err := store.List(ListOptions{Q: id})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	if strings.Join(listed.Sessions[0].Protocols, ",") != ProtocolResponses {
		t.Fatalf("protocols=%v", listed.Sessions[0].Protocols)
	}
}

func TestMalformedStartedAtDoesNotContributeToQueryLayer(t *testing.T) {
	store := openStore(t)
	good := sampleRecord("req-good", Identity{Namespace: NamespaceCodexThread, ExternalID: "trust"}, time.UnixMilli(1000), int64Ptr(8))
	good.Routing = Routing{ResolvedModel: "good-model", Provider: "good-provider"}
	cost := 1.0
	good.Usage.Cost = &cost
	good.Usage.Currency = "USD"
	id, err := store.Record(good)
	if err != nil {
		t.Fatal(err)
	}
	ghost := sampleRecord("req-ghost", Identity{Namespace: NamespaceCodexThread, ExternalID: "trust"}, time.UnixMilli(2000), int64Ptr(99))
	ghost.Routing = Routing{ResolvedModel: "ghost-model", Provider: "ghost-provider"}
	ghost.Usage.Cost = &cost
	ghost.Usage.Currency = "USD"
	if _, err := store.Record(ghost); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET started_at = 'garbage' WHERE id = 'req-ghost'`)
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Requests) != 1 || detail.Requests[0].ID != "req-good" {
		t.Fatalf("history=%#v", detail.Requests)
	}
	if containsAll(detail.Aggregates.Models, "ghost-model") || containsAll(detail.Aggregates.Providers, "ghost-provider") {
		t.Fatalf("aggregates=%#v", detail.Aggregates)
	}
	if !containsAll(detail.Aggregates.Models, "good-model") {
		t.Fatalf("lost good model %#v", detail.Aggregates.Models)
	}
	if detail.Aggregates.Usage.InputTokens.Complete || detail.Aggregates.Usage.InputTokens.TotalRequests != 2 {
		t.Fatalf("coverage=%#v", detail.Aggregates.Usage.InputTokens)
	}
	listed, err := store.List(ListOptions{Model: "ghost-model"})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("model filter=%#v err=%v", listed, err)
	}
	listed, err = store.List(ListOptions{Provider: "ghost-provider"})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("provider filter=%#v err=%v", listed, err)
	}
	values, err := store.FilterValues()
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(values.Models, "ghost-model") || containsAll(values.Providers, "ghost-provider") {
		t.Fatalf("filters=%#v", values)
	}
}

func TestMalformedStatusIsUntrustedLikeStartedAt(t *testing.T) {
	store := openStore(t)
	good := sampleRecord("req-ok-status", Identity{Namespace: NamespaceCodexThread, ExternalID: "status"}, time.UnixMilli(1000), nil)
	good.Routing = Routing{ResolvedModel: "keep-model", Provider: "keep-provider"}
	id, err := store.Record(good)
	if err != nil {
		t.Fatal(err)
	}
	bad := sampleRecord("req-bad-status", Identity{Namespace: NamespaceCodexThread, ExternalID: "status"}, time.UnixMilli(2000), nil)
	bad.Routing = Routing{ResolvedModel: "drop-model", Provider: "drop-provider"}
	if _, err := store.Record(bad); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET status = 'nope' WHERE id = 'req-bad-status'`)
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(detail.Aggregates.Models, "drop-model") {
		t.Fatalf("status corrupt leaked %#v", detail.Aggregates.Models)
	}
	listed, err := store.List(ListOptions{Model: "drop-model"})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("filter=%#v err=%v", listed, err)
	}
}

func TestUnknownProtocolIsNotAFilterValue(t *testing.T) {
	store := openStore(t)
	in := sampleRecord("req-unknown", Identity{Namespace: NamespaceCodexThread, ExternalID: "unknown-proto"}, time.UnixMilli(1000), nil)
	if _, err := store.Record(in); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET protocol = 'nope' WHERE id = 'req-unknown'`)
	values, err := store.FilterValues()
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(values.Protocols, "nope") {
		t.Fatalf("unknown protocol in filters %#v", values.Protocols)
	}
	listed, err := store.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	if len(listed.Sessions[0].Protocols) != 0 {
		t.Fatalf("summary protocols=%v", listed.Sessions[0].Protocols)
	}
}

func TestMalformedAttemptsJSONDoesNotCountFailover(t *testing.T) {
	store := openStore(t)
	in := sampleRecord("req-attempts", Identity{Namespace: NamespaceCodexThread, ExternalID: "attempts"}, time.UnixMilli(1000), nil)
	in.Attempts = []Attempt{{Member: "openai-apikey/gpt-5.4", Status: 503, Decision: "hop"}}
	id, err := store.Record(in)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET attempts_json = '{not-json' WHERE id = 'req-attempts'`)
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Aggregates.FailoverRequestCount != 0 || detail.Aggregates.HadFailover {
		t.Fatalf("failover=%#v", detail.Aggregates)
	}
	ghost := sampleRecord("req-ghost-hop", Identity{Namespace: NamespaceCodexThread, ExternalID: "attempts"}, time.UnixMilli(2000), nil)
	ghost.Attempts = []Attempt{{Member: "openai-apikey/gpt-5.4", Status: 503, Decision: "hop"}}
	if _, err := store.Record(ghost); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET started_at = 'garbage' WHERE id = 'req-ghost-hop'`)
	detail, err = store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Aggregates.FailoverRequestCount != 0 || detail.Aggregates.HadFailover {
		t.Fatalf("untrusted hop counted %#v", detail.Aggregates)
	}
}

func TestWrongShapedAttemptsJSONDoesNotFailAggregates(t *testing.T) {
	cases := []struct {
		name     string
		json     string
		failover int
	}{
		{name: "object", json: `{"decision":"hop"}`, failover: 0},
		{name: "scalar-array", json: `["hop"]`, failover: 0},
		{name: "json-string", json: `"hop"`, failover: 0},
		{name: "mixed", json: `["hop",null,123,{"decision":"committed"},{"decision":"hop"}]`, failover: 1},
		{name: "multi-hop", json: `[{"decision":"hop"},{"decision":"hop"}]`, failover: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			in := sampleRecord("req-shape", Identity{Namespace: NamespaceCodexThread, ExternalID: "shape-" + tc.name}, time.UnixMilli(1000), int64Ptr(7))
			in.Routing = Routing{ResolvedModel: "keep-model", Provider: "keep-provider"}
			in.Attempts = []Attempt{{Member: "openai-apikey/gpt-5.4", Status: 503, Decision: "hop"}}
			id, err := store.Record(in)
			if err != nil {
				t.Fatal(err)
			}
			execSQL(t, store, `UPDATE requests SET attempts_json = ? WHERE id = 'req-shape'`, tc.json)
			detail, err := store.Get(id, DetailOptions{})
			if err != nil {
				t.Fatalf("detail err=%v", err)
			}
			if len(detail.Requests) != 1 || detail.Requests[0].ID != "req-shape" {
				t.Fatalf("history=%#v", detail.Requests)
			}
			if len(detail.Requests[0].Attempts) != 0 && tc.failover == 0 {
				t.Fatalf("history attempts=%#v", detail.Requests[0].Attempts)
			}
			if !containsAll(detail.Aggregates.Models, "keep-model") || !containsAll(detail.Aggregates.Providers, "keep-provider") {
				t.Fatalf("unrelated aggregates lost %#v", detail.Aggregates)
			}
			if detail.Aggregates.Usage.InputTokens.Value == nil || *detail.Aggregates.Usage.InputTokens.Value != 7 || !detail.Aggregates.Usage.InputTokens.Complete {
				t.Fatalf("usage=%#v", detail.Aggregates.Usage.InputTokens)
			}
			if detail.Aggregates.FailoverRequestCount != tc.failover || detail.Aggregates.HadFailover != (tc.failover > 0) {
				t.Fatalf("failover=%#v want=%d", detail.Aggregates, tc.failover)
			}
		})
	}
}

func TestCostRequiresMatchingCurrency(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "cost-fx"}
	usd := sampleRecord("req-usd", identity, time.UnixMilli(1000), int64Ptr(1))
	usdCost := 1.0
	usd.Usage.Cost = &usdCost
	usd.Usage.Currency = "USD"
	id, err := store.Record(usd)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Aggregates.Usage.Cost.Complete || detail.Aggregates.Usage.Cost.Value == nil || *detail.Aggregates.Usage.Cost.Value != 1.0 || detail.Aggregates.Usage.Cost.Currency != "USD" {
		t.Fatalf("usd complete=%#v", detail.Aggregates.Usage.Cost)
	}
	missing := sampleRecord("req-missing", identity, time.UnixMilli(2000), int64Ptr(1))
	if _, err := store.Record(missing); err != nil {
		t.Fatal(err)
	}
	detail, err = store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Aggregates.Usage.Cost.Complete || detail.Aggregates.Usage.Cost.Value == nil || *detail.Aggregates.Usage.Cost.Value != 1.0 {
		t.Fatalf("missing cost=%#v", detail.Aggregates.Usage.Cost)
	}
	orphan := sampleRecord("req-orphan", identity, time.UnixMilli(3000), int64Ptr(1))
	orphanCost := 2.0
	orphan.Usage.Cost = &orphanCost
	orphan.Usage.Currency = "USD"
	if _, err := store.Record(orphan); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET currency = NULL WHERE id = 'req-orphan'`)
	detail, err = store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cost := detail.Aggregates.Usage.Cost
	if cost.Complete || cost.Value == nil || *cost.Value != 1.0 || cost.Currency != "USD" {
		t.Fatalf("null currency combined=%#v", cost)
	}
	typed := sampleRecord("req-typed", identity, time.UnixMilli(4000), int64Ptr(1))
	typed.Usage.Cost = &orphanCost
	typed.Usage.Currency = "USD"
	if _, err := store.Record(typed); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET currency = x'00' WHERE id = 'req-typed'`)
	detail, err = store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Aggregates.Usage.Cost.Complete || detail.Aggregates.Usage.Cost.Value == nil || *detail.Aggregates.Usage.Cost.Value != 1.0 {
		t.Fatalf("typed currency=%#v", detail.Aggregates.Usage.Cost)
	}
}

func TestQueryLayerCorruptionSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sessions.sqlite"
	store, err := open(path, func() time.Time { return time.UnixMilli(10_000_000) })
	if err != nil {
		t.Fatal(err)
	}
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "reopen-trust"}
	good := sampleRecord("req-keep", identity, time.UnixMilli(1000), int64Ptr(4))
	good.Routing = Routing{ResolvedModel: "keep-model", Provider: "keep-provider"}
	cost := 1.5
	good.Usage.Cost = &cost
	good.Usage.Currency = "USD"
	id, err := store.Record(good)
	if err != nil {
		t.Fatal(err)
	}
	ghost := sampleRecord("req-drop", identity, time.UnixMilli(2000), int64Ptr(9))
	ghost.Routing = Routing{ResolvedModel: "drop-model", Provider: "drop-provider"}
	if _, err := store.Record(ghost); err != nil {
		t.Fatal(err)
	}
	execSQL(t, store, `UPDATE requests SET started_at = 'garbage' WHERE id = 'req-drop'`)
	first, err := store.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := open(path, func() time.Time { return time.UnixMilli(10_000_000) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	second, err := reopened.Get(id, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(second.Aggregates.Models, "drop-model") || second.Aggregates.Usage.Cost.Complete {
		t.Fatalf("reopen leaked %#v", second.Aggregates)
	}
	if strings.Join(second.Aggregates.Models, ",") != strings.Join(first.Aggregates.Models, ",") {
		t.Fatalf("models changed")
	}
}

func execSQL(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, err := store.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func containsAll(values []string, want ...string) bool {
	have := map[string]bool{}
	for _, value := range values {
		have[value] = true
	}
	for _, value := range want {
		if !have[value] {
			return false
		}
	}
	return true
}
