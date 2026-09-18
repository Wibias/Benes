package config

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestMissingConfigMaterializesCanonicalOpenAIPoolFactoryDefault(t *testing.T) {
	cfg, err := LoadDiskConfig(filepath.Join(t.TempDir(), "missing.json"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != DiskConfigSourceDefault {
		t.Fatalf("source=%q", cfg.Source)
	}
	if len(cfg.Providers) != 1 || cfg.Providers["openai"] == nil {
		t.Fatalf("providers=%#v", cfg.Providers)
	}
	if cfg.DataPlaneTokens != nil || cfg.CORSAllowOrigins != nil {
		t.Fatalf("default credentials/origins tokens=%#v origins=%#v", cfg.DataPlaneTokens, cfg.CORSAllowOrigins)
	}

	var root struct {
		DefaultProvider string                     `json:"defaultProvider"`
		Providers       map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(cfg.Raw, &root); err != nil {
		t.Fatalf("raw default JSON: %v", err)
	}
	if root.DefaultProvider != "openai" || len(root.Providers) != 1 {
		t.Fatalf("raw default=%s", cfg.Raw)
	}

	projection := ProjectProviderSpecs(cfg)
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.ID != "openai" ||
		spec.Protocol != providerregistry.ProtocolOpenAIResponses ||
		spec.AuthMode != providerregistry.AuthModeForward ||
		spec.CodexAccountMode != providerregistry.CodexAccountModePool ||
		spec.Endpoint != canonicalForwardBaseURL+"/responses" ||
		spec.APIKey != "" {
		t.Fatalf("spec=%#v", spec)
	}
}

func TestExplicitEmptyConfigDoesNotMaterializeFactoryProvider(t *testing.T) {
	path := writeConfigFixture(t, `{}`)
	cfg, err := LoadDiskConfig(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != DiskConfigSourceFile || len(cfg.Providers) != 0 || string(cfg.Raw) != "{}" {
		t.Fatalf("cfg=%#v raw=%q", cfg, cfg.Raw)
	}
}
