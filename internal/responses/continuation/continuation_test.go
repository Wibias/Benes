package continuation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestDestinationIdentityIsRestartStableAndEndpointScoped(t *testing.T) {
	a, err := DestinationIdentity("HTTPS://Example.COM:443/v1/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := DestinationIdentity("https://example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := DestinationIdentity("https://example.com/v2")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("equivalent endpoints differ: %q %q", a, b)
	}
	if a == other {
		t.Fatal("different endpoints shared durable destination identity")
	}
}

func TestDurableCredentialIdentityIsSaltedFullWidthAndScoped(t *testing.T) {
	saltA := []byte("01234567890123456789012345678901")
	saltB := []byte("abcdefghijklmnopqrstuvwxyzABCDEF")
	secret := []byte("sk-super-secret")
	a, err := DurableKeyIdentity(secret, saltA)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := DurableKeyIdentity(secret, saltA)
	b, _ := DurableKeyIdentity(secret, saltB)
	if a != again || a == b {
		t.Fatalf("identity stability/salt scope broken: %q %q %q", a, again, b)
	}
	if strings.Contains(a, string(secret)) || len(strings.TrimPrefix(a, "key:")) != 64 {
		t.Fatalf("identity leaks/truncates credential material: %q", a)
	}
	if _, err := DurableKeyIdentity(secret, nil); !errors.Is(err, ErrDurableIdentityUnavailable) {
		t.Fatalf("missing salt = %v", err)
	}
}

func TestEphemeralCredentialIdentityIsProcessLocalAndNotDurable(t *testing.T) {
	secret := []byte("Bearer rotating-token")
	saltA := []byte("01234567890123456789012345678901")
	saltB := []byte("abcdefghijklmnopqrstuvwxyzABCDEF")
	a, err := EphemeralKeyIdentity(secret, saltA)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := EphemeralKeyIdentity(secret, saltA)
	b, _ := EphemeralKeyIdentity(secret, saltB)
	if a != again || a == b || !strings.HasPrefix(a, "eph:") || DurableCredential(a) {
		t.Fatalf("ephemeral identity broken: %q %q %q", a, again, b)
	}
}

func TestOAuthDurableIdentityRequiresTrustedStableHandle(t *testing.T) {
	if _, err := DurableOAuthIdentity(""); !errors.Is(err, ErrDurableIdentityUnavailable) {
		t.Fatalf("identityless oauth = %v", err)
	}
	got, err := DurableOAuthIdentity("acct-slot-17")
	if err != nil || got != "oauth:acct-slot-17" {
		t.Fatalf("oauth identity = %q %v", got, err)
	}
}

func TestOwnerFenceIncludesDestinationAdapterModelAndCredential(t *testing.T) {
	owner := mustOwner(t, "p", "https://a.example/v1", "responses", "m", "oauth:slot-a")
	variants := []Owner{
		mustOwner(t, "p", "https://b.example/v1", "responses", "m", "oauth:slot-a"),
		mustOwner(t, "p", "https://a.example/v1", "chat", "m", "oauth:slot-a"),
		mustOwner(t, "p", "https://a.example/v1", "responses", "m2", "oauth:slot-a"),
		mustOwner(t, "p", "https://a.example/v1", "responses", "m", "oauth:slot-b"),
	}
	for _, other := range variants {
		if owner.Matches(other) {
			t.Fatalf("foreign owner matched: %#v", other)
		}
	}
}

func TestPreviousResponseReplayDedupsOnlyWithProviderOccurrenceAnchor(t *testing.T) {
	stored := Prefix{
		Items: []Occurrence{
			{Kind: "user", Payload: json.RawMessage(`{"text":"same"}`)},
			{Kind: "assistant", ID: "msg-17", Payload: json.RawMessage(`{"text":"answer"}`)},
		},
		ProviderOutputBoundary: 1,
	}
	client := append([]Occurrence(nil), stored.Items...)
	if ShouldExpandPrefix(stored, client, CompareLimits{MaxItemBytes: 1024, MaxDepth: 8}) {
		t.Fatal("exact carried prefix with provider occurrence anchor was expanded again")
	}

	client[1].ID = "msg-18"
	if !ShouldExpandPrefix(stored, client, CompareLimits{MaxItemBytes: 1024, MaxDepth: 8}) {
		t.Fatal("different provider occurrence was deduplicated by content")
	}

	idless := stored
	idless.Items = append([]Occurrence(nil), stored.Items...)
	idless.Items[1].ID = ""
	if !ShouldExpandPrefix(idless, idless.Items, CompareLimits{MaxItemBytes: 1024, MaxDepth: 8}) {
		t.Fatal("id-less provider output authorized dedup")
	}
}

func TestPreviousResponseReplayFailsClosedOnOversizedOrDeepComparison(t *testing.T) {
	stored := Prefix{Items: []Occurrence{{Kind: "assistant", ID: "msg-1", Payload: json.RawMessage(`{"nested":{"x":1}}`)}}, ProviderOutputBoundary: 0}
	if !ShouldExpandPrefix(stored, stored.Items, CompareLimits{MaxItemBytes: 4, MaxDepth: 8}) {
		t.Fatal("oversized comparison deleted possible history")
	}
	if !ShouldExpandPrefix(stored, stored.Items, CompareLimits{MaxItemBytes: 1024, MaxDepth: 1}) {
		t.Fatal("over-deep comparison deleted possible history")
	}
}

func TestStoreRejectsForeignOwnerAndLeasesBytesToActiveTurn(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := NewStore(StoreLimits{MaxEntryBytes: 128, MaxTotalBytes: 256, MaxEntries: 2, TTL: time.Hour}, func() time.Time { return now })
	owner := mustOwner(t, "p", "https://a.example", "responses", "m", "oauth:a")
	foreign := mustOwner(t, "p", "https://a.example", "responses", "m", "oauth:b")
	key := ReplayKey{Thread: "thread-1", CallID: "call-1", Owner: owner}
	if err := store.Put(Entry{Key: key, Payload: json.RawMessage(`{"opaque":"state"}`), Prefix: anchoredPrefix()}); err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1, MaxProcessBytes: 1024, MaxTurnBytes: 1024, ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 256}})
	turn, _ := budget.AcquireTurn(context.Background(), "thread-1")
	defer turn.Close()

	if _, ok, err := store.Lease(key, foreign, turn); err != nil || ok {
		t.Fatalf("foreign owner received state: ok=%v err=%v", ok, err)
	}
	lease, ok, err := store.Lease(key, owner, turn)
	if err != nil || !ok {
		t.Fatalf("owner lease = %v %v", ok, err)
	}
	if budget.Metrics().Bytes[resourcebudget.ClassContinuation] == 0 {
		t.Fatal("active continuation did not take a turn byte lease")
	}
	lease.Release()
	if budget.Metrics().Bytes[resourcebudget.ClassContinuation] != 0 {
		t.Fatal("continuation byte lease leaked")
	}
}

func TestStoreBoundsOversizeTotalCountTTLAndPinnedEviction(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	store := NewStore(StoreLimits{MaxEntryBytes: 80, MaxTotalBytes: 150, MaxEntries: 2, TTL: time.Minute}, clock)
	owner := mustOwner(t, "p", "https://a.example", "responses", "m", "oauth:a")

	if err := store.Put(Entry{Key: ReplayKey{Thread: "oversize", CallID: "x", Owner: owner}, Payload: json.RawMessage(`{"blob":"` + strings.Repeat("x", 100) + `"}`), Prefix: anchoredPrefix()}); !errors.Is(err, ErrEntryTooLarge) {
		t.Fatalf("oversize = %v", err)
	}
	putSmall(t, store, owner, "a")
	putSmall(t, store, owner, "b")

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 2})
	turn, _ := budget.AcquireTurn(context.Background(), "a")
	defer turn.Close()
	keyA := ReplayKey{Thread: "a", CallID: "call", Owner: owner}
	pinned, ok, err := store.Lease(keyA, owner, turn)
	if err != nil || !ok {
		t.Fatalf("pin a: %v %v", ok, err)
	}
	defer pinned.Release()

	putSmall(t, store, owner, "c")
	checkLease, ok, err := store.Lease(keyA, owner, turn)
	if err != nil || !ok {
		t.Fatal("pinned entry was evicted")
	}
	checkLease.Release()

	now = now.Add(2 * time.Minute)
	otherTurn, _ := budget.AcquireTurn(context.Background(), "b")
	defer otherTurn.Close()
	if lease, ok, err := store.Lease(ReplayKey{Thread: "b", CallID: "call", Owner: owner}, owner, otherTurn); err != nil || ok || lease != nil {
		t.Fatalf("expired entry remained visible: lease=%v ok=%v err=%v", lease, ok, err)
	}
}

func TestSnapshotVersionAndMalformedAnchorFailClosed(t *testing.T) {
	owner := mustOwner(t, "p", "https://a.example", "responses", "m", "oauth:a")
	store := NewStore(StoreLimits{MaxEntryBytes: 1024, MaxTotalBytes: 2048, MaxEntries: 4, TTL: time.Hour}, time.Now)
	putSmall(t, store, owner, "a")
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewStore(StoreLimits{MaxEntryBytes: 1024, MaxTotalBytes: 2048, MaxEntries: 4, TTL: time.Hour}, time.Now)
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(snapshot), `"version":4`, `"version":3`, 1)
	if err := restored.Restore([]byte(legacy)); !errors.Is(err, ErrSnapshotVersion) {
		t.Fatalf("legacy version = %v", err)
	}
	malformed := strings.Replace(string(snapshot), `"provider_output_boundary":1`, `"provider_output_boundary":99`, 1)
	if err := restored.Restore([]byte(malformed)); err != nil {
		t.Fatal(err)
	}
	if restored.Len() != 0 {
		t.Fatal("malformed persisted boundary was restored")
	}
}

func TestNativeCodexStoreFalseDefaultIsDestinationAndAuthScoped(t *testing.T) {
	canonical := "https://chatgpt.com/backend-api/codex"
	if got := ApplyNativeStoreDefault(nil, canonical, "openai-responses", "forward"); got == nil || *got {
		t.Fatalf("canonical omitted store = %#v", got)
	}
	if got := ApplyNativeStoreDefault(nil, canonical+"/responses", "openai-responses", "forward"); got == nil || *got {
		t.Fatalf("canonical Responses omitted store = %#v", got)
	}
	truth := true
	if got := ApplyNativeStoreDefault(&truth, canonical, "openai-responses", "forward"); got == nil || !*got {
		t.Fatal("explicit store:true was overridden")
	}
	if got := ApplyNativeStoreDefault(nil, canonical, "openai-responses", "api-key"); got != nil {
		t.Fatal("key-auth provider received native forward default")
	}
	if got := ApplyNativeStoreDefault(nil, "https://gateway.example/codex", "openai-responses", "forward"); got != nil {
		t.Fatal("custom gateway received native Codex default")
	}
	if got := ApplyNativeStoreDefault(nil, "https://gateway.example/codex/responses", "openai-responses", "forward"); got != nil {
		t.Fatal("custom Responses gateway received native Codex default")
	}
}

func mustOwner(t *testing.T, provider, destination, adapter, model, credential string) Owner {
	t.Helper()
	owner, err := NewOwner(provider, destination, adapter, model, credential)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func anchoredPrefix() Prefix {
	return Prefix{Items: []Occurrence{{Kind: "user", Payload: json.RawMessage(`{"text":"hi"}`)}, {Kind: "assistant", ID: "msg-1", Payload: json.RawMessage(`{"text":"ok"}`)}}, ProviderOutputBoundary: 1}
}

func putSmall(t *testing.T, store *Store, owner Owner, thread string) {
	t.Helper()
	if err := store.Put(Entry{Key: ReplayKey{Thread: thread, CallID: "call", Owner: owner}, Payload: json.RawMessage(`{"s":1}`), Prefix: anchoredPrefix()}); err != nil {
		t.Fatal(err)
	}
}
