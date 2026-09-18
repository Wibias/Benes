package credentials

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusNeverIncludesSecretMaterial(t *testing.T) {
	store := NewOSStore(&memBackend{})
	ref, err := store.Put("slot-a", []byte("sk-secret"))
	if err != nil {
		t.Fatal(err)
	}
	st := StatusOf(store, ref)
	if !st.Available || st.ID != "slot-a" || st.Source != SourceSecureStore {
		t.Fatalf("status=%#v", st)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret") || strings.Contains(string(raw), "secret") && strings.Contains(string(raw), "sk-") {
		t.Fatalf("secret leaked in status json: %s", raw)
	}
	missing := StatusOf(store, Ref{ID: "missing", Source: SourceSecureStore})
	if missing.Available || missing.ID != "missing" {
		t.Fatalf("missing=%#v", missing)
	}
}

func TestStatusReportsUnavailableStoreWithoutSecret(t *testing.T) {
	store := NewOSStore(&memBackend{avail: ErrSecureStoreUnavailable})
	st := StatusOf(store, Ref{ID: "slot-a", Source: SourceSecureStore})
	if st.Available || st.Error == "" {
		t.Fatalf("status=%#v", st)
	}
	if strings.Contains(st.Error, "sk-") {
		t.Fatalf("error leaked secret: %s", st.Error)
	}
}
