package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
)

func TestCodexPoolSnapshotCarriesPersistedRoutingPolicy(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	store, err := codexauth.NewManagedCredentialStore(benesHome)
	if err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	auto := 0.0
	failover := 0
	source := &codexPoolSnapshotSource{
		accounts: codexauth.ManagedAccountConfig{},
		policy: codexauth.PoolRoutingPolicy{
			Strategy:            codexauth.PoolStrategyRoundRobin,
			StickyLimit:         9,
			AutoSwitchThreshold: &auto,
			FailoverThreshold:   &failover,
		},
		store: store,
		main:  main,
		now:   func() time.Time { return now },
	}
	request, err := source.Snapshot(context.Background(), openairesponses.CodexPoolDispatchIdentity{ModelID: "gpt-5.6"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Selection.Strategy != source.policy.Strategy || request.Selection.StickyLimit != 9 {
		t.Fatalf("selection=%#v", request.Selection)
	}
	if request.Selection.AutoSwitchThreshold == nil || *request.Selection.AutoSwitchThreshold != 0 {
		t.Fatalf("auto=%v", request.Selection.AutoSwitchThreshold)
	}
	if request.Selection.FailoverThreshold == nil || *request.Selection.FailoverThreshold != 0 {
		t.Fatalf("failover=%v", request.Selection.FailoverThreshold)
	}
}

func TestBuildCodexPoolAuthoritiesRejectsInvalidRoutingPolicyOnlyWhenPoolNeeded(t *testing.T) {
	disk := poolDiskForTest(t, "{\"accountPoolStickyLimit\":0}")
	if authorities, err := buildCodexPoolAuthorities(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: t.TempDir(),
		CodexHome: t.TempDir(),
	}); err == nil || authorities != nil {
		t.Fatalf("Pool authorities=%#v err=%v", authorities, err)
	}

	direct := providerregistry.Spec{
		ID: "openai", Protocol: providerregistry.ProtocolOpenAIResponses,
		AuthMode: providerregistry.AuthModeForward, CodexAccountMode: providerregistry.CodexAccountModeDirect,
	}
	if authorities, err := buildCodexPoolAuthorities(context.Background(), disk, []providerregistry.Spec{direct}, CodexPoolOptions{}); err != nil || authorities != nil {
		t.Fatalf("Direct authorities=%#v err=%v", authorities, err)
	}
}
