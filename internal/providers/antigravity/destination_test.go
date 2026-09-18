package antigravity

import (
	"errors"
	"testing"
)

func TestResolveDestinationRewritesCleartextKnownPeersAndRejectsOthers(t *testing.T) {
	got, err := ResolveDestination("")
	if err != nil || got != DailyAPI {
		t.Fatalf("default=%q %v", got, err)
	}
	httpsDaily, err := ResolveDestination(DailyAPI + "/")
	if err != nil || httpsDaily != DailyAPI {
		t.Fatalf("https daily=%q %v", httpsDaily, err)
	}
	rewritten, err := ResolveDestination("http://daily-cloudcode-pa.googleapis.com")
	if err != nil || rewritten != DailyAPI {
		t.Fatalf("cleartext rewrite=%q %v", rewritten, err)
	}
	if _, err := ResolveDestination("http://evil.example"); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("cleartext foreign=%v", err)
	}
	if _, err := ResolveDestination("https://evil.example"); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("https foreign=%v", err)
	}
	if _, err := ResolveDestination("https://user:token@daily-cloudcode-pa.googleapis.com"); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("userinfo=%v", err)
	}
}

func TestPeerIsOneWayKnownPair(t *testing.T) {
	peer, ok := Peer(DailyAPI)
	if !ok || peer != ProdAPI {
		t.Fatalf("daily peer=%q %v", peer, ok)
	}
	peer, ok = Peer(ProdAPI)
	if !ok || peer != DailyAPI {
		t.Fatalf("prod peer=%q %v", peer, ok)
	}
	if _, ok := Peer("https://example"); ok {
		t.Fatal("unknown peer")
	}
}
