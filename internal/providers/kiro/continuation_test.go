package kiro

import (
	"context"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func kiroTestAuthority(t *testing.T) *continuation.Authority {
	t.Helper()
	store := continuation.NewStore(continuation.StoreLimits{
		MaxEntryBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxEntries: 32, TTL: time.Hour,
	}, time.Now)
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = 'k'
	}
	authority, err := continuation.NewAuthority(store, nil, salt)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func kiroTestTurn(t *testing.T) *resourcebudget.Turn {
	t.Helper()
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 4, MaxProcessBytes: 4 << 20, MaxTurnBytes: 4 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20},
	})
	turn, err := budget.AcquireTurn(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	return turn
}

func TestKiroContinuationOwnerIncludesProfileRegionAndSurvivesTokenRefresh(t *testing.T) {
	authority := kiroTestAuthority(t)
	turn := kiroTestTurn(t)
	profile := "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc"
	first := &Client{
		account: AccountSnapshot{
			AccessToken: "tok-old",
			ProfileARN:  profile,
			APIRegion:   "us-east-1",
		},
		endpoint:     "https://runtime.us-east-1.kiro.dev",
		continuation: authority,
	}
	bound, err := first.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", UpstreamModelID: "claude-sonnet-4"},
		Turn:   turn,
	})
	if err != nil || bound == nil {
		t.Fatalf("bind=%v err=%v", bound, err)
	}
	owner := bound.Owner
	bound.Release()

	refreshed := &Client{
		account: AccountSnapshot{
			AccessToken: "tok-new",
			ProfileARN:  profile,
			APIRegion:   "us-east-1",
		},
		endpoint:     "https://runtime.us-east-1.kiro.dev",
		continuation: authority,
	}
	again, err := refreshed.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", UpstreamModelID: "claude-sonnet-4"},
		Turn:   turn,
	})
	if err != nil || again == nil {
		t.Fatalf("refresh bind=%v err=%v", again, err)
	}
	if !again.Owner.Matches(owner) {
		t.Fatalf("token refresh changed owner: %#v vs %#v", owner, again.Owner)
	}
	again.Release()

	west := &Client{
		account: AccountSnapshot{
			AccessToken: "tok-new",
			ProfileARN:  profile,
			APIRegion:   "us-west-2",
		},
		endpoint:     "https://runtime.us-west-2.kiro.dev",
		continuation: authority,
	}
	otherRegion, err := west.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", UpstreamModelID: "claude-sonnet-4"},
		Turn:   turn,
	})
	if err != nil || otherRegion == nil {
		t.Fatalf("region bind=%v err=%v", otherRegion, err)
	}
	if otherRegion.Owner.Matches(owner) {
		t.Fatal("region change shared continuation owner")
	}
	otherRegion.Release()

	otherProfile := &Client{
		account: AccountSnapshot{
			AccessToken: "tok-new",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/other",
			APIRegion:   "us-east-1",
		},
		endpoint:     "https://runtime.us-east-1.kiro.dev",
		continuation: authority,
	}
	other, err := otherProfile.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", UpstreamModelID: "claude-sonnet-4"},
		Turn:   turn,
	})
	if err != nil || other == nil {
		t.Fatalf("profile bind=%v err=%v", other, err)
	}
	if other.Owner.Matches(owner) {
		t.Fatal("profile change shared continuation owner")
	}
	other.Release()
}
