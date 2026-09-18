package cursor

import (
	"testing"
	"time"
)

func TestCheckpointLookupFailsClosedOnAmbiguousOrMismatch(t *testing.T) {
	store := NewCheckpointStore()
	digest := PrefixDigest([]string{"sys"}, []string{"hi"})
	store.Remember(Checkpoint{ConversationID: "c1", Identity: "acct", Model: "gpt-5.4", PrefixDigest: digest, Bytes: []byte("ckpt"), StoredAt: time.Now()})
	if _, ok := store.Lookup("c1", "acct", "gpt-5.4", digest); !ok {
		t.Fatal("expected hit")
	}
	if _, ok := store.Lookup("c1", "acct", "other", digest); ok {
		t.Fatal("model mismatch")
	}
	store.Remember(Checkpoint{ConversationID: "c1", Identity: "acct", Model: "gpt-5.4", PrefixDigest: digest, Bytes: []byte("dup"), StoredAt: time.Now()})
	if _, ok := store.Lookup("c1", "acct", "gpt-5.4", digest); ok {
		t.Fatal("ambiguous match must fail closed")
	}
}

func TestRecomputeEstimatedTotalIgnoresCacheRead(t *testing.T) {
	if got := RecomputeEstimatedTotal(10, 4, 99); got != 14 {
		t.Fatalf("total=%d", got)
	}
}
