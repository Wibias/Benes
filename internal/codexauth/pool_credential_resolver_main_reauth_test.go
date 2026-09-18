package codexauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPoolCredentialResolverMarksMissingPhysicalMainForReauth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	main := &fakeMainCredentialReader{result: MainCredentialResult{Status: MainCredentialMissing}}
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector: selector,
		ManagedTokens: &fakeManagedTokenGetter{},
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()},
		MainCredentials: main,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = MainCredentialResult{
		Status: MainCredentialOK,
		Credential: MainCredential{AccessToken: "snapshot-only"},
	}

	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: input,
		WriterGeneration: 17,
	})
	if !errors.Is(err, ErrPoolSelectedCredentialUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if !selector.Reauth.Needs(MainAccountID) {
		t.Fatal("missing physical main credential did not mark reauth")
	}
	if main.calls != 1 {
		t.Fatalf("main reads=%d want=1", main.calls)
	}
}
