package gatewayrouting

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveModelOverrideAndEmptyPolicy(t *testing.T) {
	settings := Settings{
		Provider: Policy{Only: []string{"anthropic"}, Sort: SortCost},
		Models: map[string]Policy{
			"anthropic/claude-sonnet-5": {Order: []string{"vertex", "anthropic"}},
		},
	}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	got := settings.Resolve("anthropic/claude-sonnet-5")
	if len(got.Only) != 0 || got.Sort != "" || strings.Join(got.Order, ",") != "vertex,anthropic" {
		t.Fatalf("override must replace provider defaults: %+v", got)
	}
	fallback := settings.Resolve("other")
	if fallback.Sort != SortCost || strings.Join(fallback.Only, ",") != "anthropic" || len(fallback.Order) != 0 {
		t.Fatalf("provider default %+v", fallback)
	}
	if !(Policy{}).Empty() {
		t.Fatal("empty")
	}
}

func TestValidateRejectsBadSortAndSlugs(t *testing.T) {
	if err := (Policy{Sort: "fast"}).Validate(); err == nil {
		t.Fatal("sort fast")
	}
	if err := (Policy{Only: []string{"Anthropic"}}).Validate(); err == nil {
		t.Fatal("uppercase slug")
	}
	if err := (Policy{Order: []string{" anthropic"}}).Validate(); err == nil {
		t.Fatal("padded slug")
	}
	if err := (Policy{Only: []string{"anthropic", "anthropic"}}).Validate(); err == nil {
		t.Fatal("duplicate")
	}
}

func TestApplyVercelChatCanonicalAndLookalike(t *testing.T) {
	base := []byte(`{"model":"anthropic/claude-sonnet-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	policy := Policy{Only: []string{"anthropic"}, Order: []string{"anthropic"}, Sort: SortTTFT}
	canonical := "https://ai-gateway.vercel.sh/v1/chat/completions"
	got, err := ApplyVercelChat(base, canonical, policy)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if json.Unmarshal(got, &payload) != nil {
		t.Fatal("decode")
	}
	gateway := payload["providerOptions"].(map[string]any)["gateway"].(map[string]any)
	if gateway["sort"] != SortTTFT {
		t.Fatalf("%v", gateway)
	}
	if _, ok := payload["service_tier"]; ok {
		t.Fatal("must not invent service_tier")
	}
	if _, ok := payload["provider"]; ok {
		t.Fatal("must not emit OpenRouter-style provider pin")
	}
	if _, ok := payload["allow_fallbacks"]; ok {
		t.Fatal("must not emit allow_fallbacks")
	}

	lookalike, err := ApplyVercelChat(base, "https://evil.example/ai-gateway/v1/chat/completions", policy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lookalike, base) {
		t.Fatalf("lookalike received vendor fields: %s", lookalike)
	}

	empty, err := ApplyVercelChat(base, canonical, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(empty, base) {
		t.Fatalf("empty policy must keep bytes: %s", empty)
	}
}

func TestApplyVercelChatStreamParityAndInvalidPolicy(t *testing.T) {
	nonStream := []byte(`{"model":"anthropic/claude-sonnet-5","messages":[],"stream":false}`)
	stream := []byte(`{"model":"anthropic/claude-sonnet-5","messages":[],"stream":true}`)
	policy := Policy{Sort: SortCost}
	endpoint := "https://ai-gateway.vercel.sh/v1/chat/completions"
	a, err := ApplyVercelChat(nonStream, endpoint, policy)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ApplyVercelChat(stream, endpoint, policy)
	if err != nil {
		t.Fatal(err)
	}
	var pa, pb map[string]any
	_ = json.Unmarshal(a, &pa)
	_ = json.Unmarshal(b, &pb)
	if pa["providerOptions"].(map[string]any)["gateway"].(map[string]any)["sort"] != pb["providerOptions"].(map[string]any)["gateway"].(map[string]any)["sort"] {
		t.Fatal("stream/non-stream policy mismatch")
	}
	if _, err := ApplyVercelChat(nonStream, endpoint, Policy{Sort: "fast"}); err == nil {
		t.Fatal("invalid sort must fail closed")
	}
}

func TestCanonicalVercelHost(t *testing.T) {
	if !CanonicalVercel("https://ai-gateway.vercel.sh/v1") {
		t.Fatal("canonical")
	}
	if CanonicalVercel("https://ai-gateway.vercel.sh.evil.example/v1") {
		t.Fatal("suffix lookalike")
	}
	if CanonicalVercel("https://example.com") {
		t.Fatal("other host")
	}
}
func TestDecodePolicyRejectsUnknownFieldsAndInvalidSort(t *testing.T) {
	if _, err := DecodePolicy([]byte(`{"allowFallbacks":false,"only":["anthropic"]}`)); err == nil {
		t.Fatal("unknown field")
	}
	if _, err := DecodePolicy([]byte(`{"sort":"fast"}`)); err == nil {
		t.Fatal("sort")
	}
	got, err := DecodePolicy([]byte(`{"only":["anthropic"],"sort":"ttft"}`))
	if err != nil || strings.Join(got.Only, ",") != "anthropic" || got.Sort != SortTTFT {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestDecodeModelPoliciesRequiresExactIDs(t *testing.T) {
	if _, err := DecodeModelPolicies([]byte(`{" anthropic/claude":{"sort":"cost"}}`)); err == nil {
		t.Fatal("padded model id")
	}
	got, err := DecodeModelPolicies([]byte(`{"anthropic/claude-sonnet-5":{"order":["vertex"]}}`))
	if err != nil || strings.Join(got["anthropic/claude-sonnet-5"].Order, ",") != "vertex" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestEmptyPolicyDoesNotRecordDiagnostics(t *testing.T) {
	if !(Settings{}).Empty() {
		t.Fatal("empty settings")
	}
	cloned := Settings{Provider: Policy{Only: []string{"anthropic"}}, Models: map[string]Policy{"m": {Sort: SortCost}}}.Clone()
	cloned.Provider.Only[0] = "mutated"
	cloned.Models["m"] = Policy{Sort: SortTPS}
	src := Settings{Provider: Policy{Only: []string{"anthropic"}}, Models: map[string]Policy{"m": {Sort: SortCost}}}
	if src.Provider.Only[0] != "anthropic" || src.Models["m"].Sort != SortCost {
		t.Fatal("clone alias")
	}
}
