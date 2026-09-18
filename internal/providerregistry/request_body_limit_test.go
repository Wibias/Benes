package providerregistry

import (
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/transport"
)

func TestApplySpecRequestBodyLimitScopeScopesToResponsesPathAndPreservesOptions(t *testing.T) {
	originalTimeout := 17 * time.Second
	options := transport.ClientOptions{
		ResponseHeaderTimeout: originalTimeout,
		MaxRequestBodyBytes:   1024,
	}
	got := applySpecRequestBodyLimitScope(Spec{
		Protocol:             ProtocolOpenAIResponses,
		Endpoint:             "https://example.com/v1/responses",
		MaxUpstreamBodyBytes: 1024,
	}, options)
	if got.MaxRequestBodyBytes != 1024 {
		t.Fatalf("max=%d", got.MaxRequestBodyBytes)
	}
	if got.RequestBodyLimitPath != "/v1/responses" {
		t.Fatalf("path=%q", got.RequestBodyLimitPath)
	}
	if got.ResponseHeaderTimeout != originalTimeout {
		t.Fatalf("response timeout=%s", got.ResponseHeaderTimeout)
	}
}

func TestApplySpecRequestBodyLimitScopeNormalizesRootEndpointPath(t *testing.T) {
	got := applySpecRequestBodyLimitScope(Spec{
		Protocol:             ProtocolOpenAIResponses,
		Endpoint:             "https://example.com",
		MaxUpstreamBodyBytes: 1024,
	}, transport.ClientOptions{MaxRequestBodyBytes: 1024})
	if got.RequestBodyLimitPath != "/" {
		t.Fatalf("path=%q", got.RequestBodyLimitPath)
	}
}

func TestApplySpecRequestBodyLimitScopeDisabledLeavesOptionsUntouched(t *testing.T) {
	options := transport.ClientOptions{MaxRequestBodyBytes: 77, RequestBodyLimitPath: "/existing"}
	got := applySpecRequestBodyLimitScope(Spec{Endpoint: "https://example.com/v1/responses"}, options)
	if got.MaxRequestBodyBytes != options.MaxRequestBodyBytes || got.RequestBodyLimitPath != options.RequestBodyLimitPath {
		t.Fatalf("got=%#v want=%#v", got, options)
	}
}
