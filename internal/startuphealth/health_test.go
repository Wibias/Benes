package startuphealth

import (
	"testing"

	"github.com/Wibias/Benes/internal/codexrouting"
)

func TestDeriveNative(t *testing.T) {
	got := Derive(Inputs{RoutingKind: codexrouting.KindNative, Platform: "windows"})
	if got.Status != StatusNative || got.LocalRoutingDependency || got.RecommendedCommand != nil {
		t.Fatalf("%#v", got)
	}
}

func TestDeriveProtectedService(t *testing.T) {
	got := Derive(Inputs{
		RoutingKind:      codexrouting.KindBenesLocal,
		ServiceViable:    true,
		ServiceSupported: true,
		ServiceInstalled: true,
		Platform:         "windows",
	})
	if got.Status != StatusProtected || got.Protection != ProtectionService || !got.RebootSafe {
		t.Fatalf("%#v", got)
	}
}

func TestDeriveAtRiskRecommendsInstall(t *testing.T) {
	got := Derive(Inputs{
		RoutingKind:      codexrouting.KindBenesLocal,
		ServiceSupported: true,
		Platform:         "windows",
	})
	if got.Status != StatusAtRisk || got.RecommendedCommand == nil || *got.RecommendedCommand != "benes service install" {
		t.Fatalf("%#v", got)
	}
}
