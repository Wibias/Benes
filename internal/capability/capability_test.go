package capability

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestProviderDenyCannotBeReopenedByExactAllow(t *testing.T) {
	falseVal := false
	policy := Policy{
		Protocol:                 "openai-chat",
		SupportsServiceTier:      &falseVal,
		ModelSupportsServiceTier: map[string]bool{"gpt-5.6": true},
		ChatServiceTier:          true,
	}
	tier := "priority"
	if got := DecideServiceTier(policy, "gpt-5.6", &tier); got != nil {
		t.Fatalf("deny reopened: %v", got)
	}
}

func TestExactFastAllowDoesNotAuthorizeFlex(t *testing.T) {
	policy := Policy{
		Protocol:                 "openai-responses",
		ModelSupportsServiceTier: map[string]bool{"gpt-5.6": true},
	}
	flex := "flex"
	if got := DecideServiceTier(policy, "gpt-5.6", &flex); got != nil {
		t.Fatalf("flex leaked from Fast allow: %v", got)
	}
	priority := "priority"
	if got := DecideServiceTier(policy, "gpt-5.6", &priority); got == nil || *got != "priority" {
		t.Fatalf("priority=%v", got)
	}
}

func TestChatUnclassifiedStripsUnlessCallerForwardOrExactAllow(t *testing.T) {
	tier := "priority"
	unclassified := Policy{Protocol: "openai-chat"}
	if got := DecideServiceTier(unclassified, "gpt-5.6", &tier); got != nil {
		t.Fatalf("unclassified forwarded: %v", got)
	}
	forward := Policy{Protocol: "openai-chat", ChatServiceTier: true}
	if got := DecideServiceTier(forward, "gpt-5.6", &tier); got == nil || *got != "priority" {
		t.Fatalf("chatServiceTier=%v", got)
	}
}

func TestOpenRouterPriorityOnlyOnCanonicalOpenAISlugs(t *testing.T) {
	policy := Policy{
		Protocol: "openai-chat",
		Endpoint: "https://openrouter.ai/api/v1/chat/completions",
	}
	tier := "priority"
	if got := DecideServiceTier(policy, "openai/gpt-5.6-sol", &tier); got == nil || *got != "priority" {
		t.Fatalf("openai slug=%v", got)
	}
	if got := DecideServiceTier(policy, "anthropic/claude-sonnet-5", &tier); got != nil {
		t.Fatalf("anthropic slug forwarded: %v", got)
	}
	custom := policy
	custom.Endpoint = "https://gateway.example/v1/chat/completions"
	if got := DecideServiceTier(custom, "openai/gpt-5.6-sol", &tier); got != nil {
		t.Fatalf("custom gateway inherited OpenRouter Fast: %v", got)
	}
}

func TestStructuredOutputOptOutIsExactModelID(t *testing.T) {
	policy := Policy{NoStructuredOutputModels: []string{"gpt-oss"}}
	if AllowStructuredOutput(policy, "gpt-oss") {
		t.Fatal("exact id should opt out")
	}
	if !AllowStructuredOutput(policy, "gpt-oss:120b") {
		t.Fatal("tagged sibling must keep structured output")
	}
}

func TestApplyStripsStructuredOutputAndKeepsXAIPriorityFailClosed(t *testing.T) {
	req := &protocol.ParsedRequest{
		UpstreamModelID: "gpt-oss",
		Options: protocol.RequestOptions{
			TextFormat:  &protocol.TextFormat{Type: "json_schema", Name: "answer"},
			ServiceTier: stringPtr("priority"),
		},
	}
	if err := Apply(req, Policy{NoStructuredOutputModels: []string{"gpt-oss"}, Protocol: "openai-chat", ChatServiceTier: true}, ""); err != nil {
		t.Fatal(err)
	}
	if req.Options.TextFormat != nil {
		t.Fatalf("text format survived: %#v", req.Options.TextFormat)
	}
	xai := &protocol.ParsedRequest{UpstreamModelID: "grok-4.6", Options: protocol.RequestOptions{ServiceTier: stringPtr("priority")}}
	if err := Apply(xai, Policy{Protocol: "openai-chat", AuthClass: "api-key", Endpoint: "https://api.x.ai.example/v1", ChatServiceTier: true}, ""); err == nil {
		t.Fatal("noncanonical xAI Priority was allowed")
	}
}

func stringPtr(v string) *string { return &v }
