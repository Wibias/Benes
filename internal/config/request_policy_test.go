package config

import (
	"testing"

	"github.com/Wibias/Benes/internal/requestpolicy"
)

func TestDecodeDiskConfigLoadsRequestPolicy(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	cfg, err := decodeDiskConfigBytes([]byte("{\"providers\":{},\"requestPolicy\":{\"serviceTier\":\"priority\"}}"), DiskConfigSourceFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequestPolicy.ServiceTier != "priority" {
		t.Fatalf("tier=%q", cfg.RequestPolicy.ServiceTier)
	}
	if got := requestpolicy.Current().ServiceTier; got != "priority" {
		t.Fatalf("published tier=%q", got)
	}
}

func TestDecodeDiskConfigRejectsInvalidRequestPolicy(t *testing.T) {
	defer requestpolicy.Publish(requestpolicy.Policy{})
	if _, err := decodeDiskConfigBytes([]byte("{\"providers\":{},\"requestPolicy\":{\"serviceTier\":\"fast\"}}"), DiskConfigSourceFile); err == nil {
		t.Fatal("expected invalid service tier to fail")
	}
}
