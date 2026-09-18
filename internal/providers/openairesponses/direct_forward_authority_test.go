package openairesponses

import (
	"context"
	"errors"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

func TestDirectForwardAuthorityRequiresCallerBearer(t *testing.T) {
	tests := []struct {
		name string
		auth string
		ok   bool
	}{
		{name: "canonical", auth: "Bearer token", ok: true},
		{name: "case insensitive", auth: "bearer token", ok: true},
		{name: "outer whitespace", auth: "  Bearer token  ", ok: true},
		{name: "tab separator", auth: "Bearer\ttoken", ok: true},
		{name: "extra text allowed by predicate", auth: "Bearer token extra", ok: true},
		{name: "missing", auth: "", ok: false},
		{name: "scheme only", auth: "Bearer", ok: false},
		{name: "other scheme", auth: "Basic token", ok: false},
		{name: "blank token", auth: "Bearer   ", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dispatch := providers.DispatchRequest{ForwardHeaders: providers.NewForwardHeaders(map[string]string{
				"authorization":      tc.auth,
				"chatgpt-account-id": "acct",
			})}
			credential, err := (DirectForwardAuthority{}).Resolve(context.Background(), dispatch)
			if tc.ok {
				if err != nil || credential.Authorization != tc.auth || credential.ChatGPTAccountID != "acct" {
					t.Fatalf("credential=%#v err=%v", credential, err)
				}
				return
			}
			if !errors.Is(err, ErrDirectAuthorizationRequired) {
				t.Fatalf("credential=%#v err=%v", credential, err)
			}
		})
	}
}

func TestDirectForwardAuthorityRejectsBlockedAdmissionCredential(t *testing.T) {
	dispatch := providers.DispatchRequest{ForwardHeaders: providers.NewForwardHeadersWithBlockedAuthorization(
		map[string]string{"authorization": "Bearer local-admission"}, true,
	)}
	credential, err := (DirectForwardAuthority{}).Resolve(context.Background(), dispatch)
	if !errors.Is(err, ErrForwardAdmissionCredential) || credential != (ForwardCredential{}) {
		t.Fatalf("credential=%#v err=%v", credential, err)
	}
}
