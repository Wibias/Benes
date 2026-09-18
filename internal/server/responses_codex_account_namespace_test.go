package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providercontract "github.com/Wibias/Benes/internal/providers"
)

type codexNamespaceCapturingProvider struct {
	seen providercontract.DispatchRequest
}

func (p *codexNamespaceCapturingProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.seen = dispatch
	return &sliceStream{}, nil
}

func TestResponsesRoutesCodexAccountNamespaceToCanonicalOpenAIWithFixedIdentity(t *testing.T) {
	provider := &codexNamespaceCapturingProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken:         "local-secret",
		Providers:              map[string]Provider{"openai": provider},
		CodexAccountNamespaces: map[string]string{"side": "acct-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"side/gpt-5.6","store":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if provider.seen.Parsed.UpstreamModelID != "gpt-5.6" || provider.seen.CodexAccountID != "acct-b" {
		t.Fatalf("dispatch=%#v", provider.seen)
	}
}
