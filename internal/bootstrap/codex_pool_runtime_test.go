package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers"
)

func TestBuildCodexPoolAuthoritiesSkipsAllPoolIOForDirectOnlySpecs(t *testing.T) {
	called := 0
	authorities, err := buildCodexPoolAuthorities(context.Background(), config.DiskConfig{Raw: json.RawMessage(`{not-json`)}, []providerregistry.Spec{{
		ID: "openai", Protocol: providerregistry.ProtocolOpenAIResponses,
		AuthMode: providerregistry.AuthModeForward, CodexAccountMode: providerregistry.CodexAccountModeDirect,
	}}, CodexPoolOptions{
		BenesHome: "relative-must-not-be-read",
		ResolveCodexHome: func() (string, error) {
			called++
			return "", errors.New("must not be called")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorities != nil || called != 0 {
		t.Fatalf("authorities=%#v resolve calls=%d", authorities, called)
	}
}

func TestBuildCodexPoolAuthoritiesSelectsFreshManagedCredentialAndIgnoresCallerAuth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"managed": map[string]any{
			"generation": 3,
			"credential": map[string]any{
				"accessToken":      "managed-access",
				"refreshToken":     "managed-refresh",
				"expiresAt":        now.Add(time.Hour).UnixMilli(),
				"chatgptAccountId": "managed-chat",
			},
		},
	})
	disk := poolDiskForTest(t, "{\"codexAccounts\":[{\"id\":\"managed\",\"email\":\"managed@example.com\",\"plan\":\"pro\"}],\"activeCodexAccountId\":\"managed\"}")

	authorities, err := buildCodexPoolAuthorities(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome,
		CodexHome: codexHome,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := authorities["openai"]
	if authority == nil {
		t.Fatal("openai Pool authority missing")
	}
	dispatch := providers.DispatchRequest{
		Parsed: protocolRequestForPoolTest("gpt-5.6"),
		ForwardHeaders: providers.NewForwardHeadersWithBlockedAuthorization(map[string]string{
			"authorization":            "Bearer attacker",
			"chatgpt-account-id":       "caller-chat",
			"x-codex-parent-thread-id": "thread-1",
		}, true),
	}
	credential, err := authority.Resolve(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer managed-access" || credential.ChatGPTAccountID != "managed-chat" {
		t.Fatalf("credential=%#v", credential)
	}
}

func TestBuildCodexPoolAuthoritiesCanSelectPhysicalMainWithoutManagedStore(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{
			"access_token": "main-access",
			"account_id":   "main-chat",
		},
	})
	disk := poolDiskForTest(t, `{}`)

	authorities, err := buildCodexPoolAuthorities(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome,
		CodexHome: codexHome,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := authorities["openai"].Resolve(context.Background(), providers.DispatchRequest{
		Parsed: protocolRequestForPoolTest("gpt-5.6"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer main-access" || credential.ChatGPTAccountID != "main-chat" {
		t.Fatalf("credential=%#v", credential)
	}
}

func TestBuildCodexPoolAuthoritiesFailsClosedWhenNoUsableCredentialExists(t *testing.T) {
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	disk := poolDiskForTest(t, `{}`)
	authorities, err := buildCodexPoolAuthorities(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome,
		CodexHome: codexHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = authorities["openai"].Resolve(context.Background(), providers.DispatchRequest{
		Parsed:         protocolRequestForPoolTest("gpt-5.6"),
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{"authorization": "Bearer caller-must-not-fallback"}),
	})
	if !errors.Is(err, codexauth.ErrPoolNoUsableAccount) {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildCodexPoolAuthoritiesRejectsMalformedPoolMetadataAndInvalidHomesBeforeRegistry(t *testing.T) {
	for _, tc := range []struct {
		name string
		disk config.DiskConfig
		opts CodexPoolOptions
	}{
		{
			name: "malformed metadata",
			disk: poolDiskForTest(t, "{\"codexAccounts\":\"wrong\"}"),
			opts: CodexPoolOptions{BenesHome: t.TempDir(), CodexHome: t.TempDir()},
		},
		{
			name: "relative benes home",
			disk: poolDiskForTest(t, `{}`),
			opts: CodexPoolOptions{BenesHome: "relative", CodexHome: t.TempDir()},
		},
		{
			name: "relative codex home",
			disk: poolDiskForTest(t, `{}`),
			opts: CodexPoolOptions{BenesHome: t.TempDir(), CodexHome: "relative"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorities, err := buildCodexPoolAuthorities(context.Background(), tc.disk, []providerregistry.Spec{poolSpecForTest()}, tc.opts)
			if err == nil || authorities != nil {
				t.Fatalf("authorities=%#v err=%v", authorities, err)
			}
		})
	}
}

func TestBuildCodexPoolAuthoritiesFailsClosedOnInvalidManagedCredentialStore(t *testing.T) {
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	path := filepath.Join(benesHome, "codex-accounts.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	authorities, err := buildCodexPoolAuthorities(context.Background(), poolDiskForTest(t, `{}`), []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome,
		CodexHome: codexHome,
	})
	if err == nil || authorities != nil {
		t.Fatalf("authorities=%#v err=%v", authorities, err)
	}
}

func poolSpecForTest() providerregistry.Spec {
	return providerregistry.Spec{
		ID: "openai", Protocol: providerregistry.ProtocolOpenAIResponses,
		AuthMode: providerregistry.AuthModeForward, CodexAccountMode: providerregistry.CodexAccountModePool,
	}
}

func poolDiskForTest(t *testing.T, root string) config.DiskConfig {
	t.Helper()
	return config.DiskConfig{Raw: json.RawMessage(root)}
}

func protocolRequestForPoolTest(model string) protocol.ParsedRequest {
	return protocol.ParsedRequest{ModelID: model, UpstreamModelID: model}
}

func writePrivateJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
