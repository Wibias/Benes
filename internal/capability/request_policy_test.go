package capability

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/requestpolicy"
)

func TestApplyUsesAdmittedServiceTierWhenClientOmitsIt(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	// Live settings must not decide an admitted request: the parameter wins.
	if err := requestpolicy.Publish(requestpolicy.Policy{ServiceTier: "flex"}); err != nil {
		t.Fatal(err)
	}
	req := protocol.ParsedRequest{ModelID: "gpt-5.6"}
	if err := Apply(&req, Policy{Protocol: "openai-responses"}, "priority"); err != nil {
		t.Fatal(err)
	}
	if req.Options.ServiceTier == nil || *req.Options.ServiceTier != "priority" {
		t.Fatalf("tier=%#v", req.Options.ServiceTier)
	}
}

func TestApplyPreservesExplicitClientServiceTier(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	explicit := "flex"
	req := protocol.ParsedRequest{ModelID: "gpt-5.6", Options: protocol.RequestOptions{ServiceTier: &explicit}}
	if err := Apply(&req, Policy{Protocol: "openai-responses"}, "priority"); err != nil {
		t.Fatal(err)
	}
	if req.Options.ServiceTier == nil || *req.Options.ServiceTier != "flex" {
		t.Fatalf("tier=%#v", req.Options.ServiceTier)
	}
}

func TestApplySuppressesConfiguredTierWhenCapabilityDeniesIt(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	denied := false
	req := protocol.ParsedRequest{ModelID: "gpt-5.6"}
	if err := Apply(&req, Policy{Protocol: "openai-responses", SupportsServiceTier: &denied}, "priority"); err != nil {
		t.Fatal(err)
	}
	if req.Options.ServiceTier != nil {
		t.Fatalf("unsupported destination received tier %q", *req.Options.ServiceTier)
	}
}

func TestApplySuppressesConfiguredTierForChatWithoutSerializationSupport(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	req := protocol.ParsedRequest{ModelID: "gpt-5.6"}
	if err := Apply(&req, Policy{Protocol: "openai-chat"}, "priority"); err != nil {
		t.Fatal(err)
	}
	if req.Options.ServiceTier != nil {
		t.Fatalf("chat destination without tier support received %q", *req.Options.ServiceTier)
	}
}

