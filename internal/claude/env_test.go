package claude

import (
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestBuildEnvInjectsProxyMarkerWithoutUserKey(t *testing.T) {
	env := BuildEnv(BuildInput{
		Port:     18080,
		Settings: CodeSettings{AuthMode: "proxy"},
		Env:      map[string]string{},
	})
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:18080" {
		t.Fatalf("base=%s", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != ProxyMarker {
		t.Fatalf("token=%s", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["ANTHROPIC_API_KEY"] != "" {
		t.Fatal("must not set ANTHROPIC_API_KEY")
	}
	if env["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] != "1" {
		t.Fatal("host managed missing")
	}
}

func TestBuildEnvKeepsUserAPIKeyAndDropsOwnAdmission(t *testing.T) {
	env := BuildEnv(BuildInput{
		Port:      18080,
		Settings:  CodeSettings{AuthMode: "auto"},
		OwnTokens: []string{"benes-secret"},
		Env: map[string]string{
			"ANTHROPIC_API_KEY":    "sk-ant-user",
			"ANTHROPIC_AUTH_TOKEN": "benes-secret",
		},
	})
	if env["ANTHROPIC_API_KEY"] != "sk-ant-user" {
		t.Fatalf("key=%s", env["ANTHROPIC_API_KEY"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatalf("token leaked=%s", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] == "1" {
		t.Fatal("must not claim host auth when user key present")
	}
}

func TestBuildEnvStripsAdmissionShapedAPIKey(t *testing.T) {
	env := BuildEnv(BuildInput{
		Port:      18080,
		Settings:  CodeSettings{AuthMode: "proxy"},
		OwnTokens: []string{"client-secret"},
		Env:       map[string]string{"ANTHROPIC_API_KEY": "client-secret"},
	})
	if env["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("api key=%s", env["ANTHROPIC_API_KEY"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "client-secret" {
		t.Fatalf("token=%s", env["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestBuildEnvDoesNotEchoSecretsInModelSlots(t *testing.T) {
	env := BuildEnv(BuildInput{
		Port: 18080,
		Settings: CodeSettings{
			AuthMode:       "subscription",
			Model:          "openai-apikey/gpt-5.5",
			SmallFastModel: "gpt-5.4-mini",
		},
		Env: map[string]string{"ANTHROPIC_API_KEY": "sk-ant-user"},
	})
	joined := ""
	for k, v := range env {
		joined += k + "=" + v + "\n"
	}
	if strings.Contains(joined, "sk-ant-user") && strings.Contains(env["ANTHROPIC_MODEL"], "sk-") {
		t.Fatal(joined)
	}
	if env["ANTHROPIC_MODEL"] != "openai-apikey/gpt-5.5" {
		t.Fatalf("model=%s", env["ANTHROPIC_MODEL"])
	}
}

func TestIsProxyAdmissionSecretRecognizesPrefixedTokens(t *testing.T) {
	if !IsProxyAdmissionSecret("benes_data_abc", nil) || !IsProxyAdmissionSecret("benes_"+strings.Repeat("a", 40), nil) {
		t.Fatal("prefix")
	}
	if IsProxyAdmissionSecret("sk-ant-user", nil) {
		t.Fatal("user key treated as ours")
	}
}
