package codexauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestManagedCredentialStoreNormalizesRefreshFingerprintAndValidationMetadata(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{
		"legacy": {
			"accessToken":"a",
			"refreshToken":"refresh-legacy",
			"expiresAt":1700000000000,
			"chatgptAccountId":"chat-a"
		},
		"current": {
			"generation":4,
			"credential":{"accessToken":"b","refreshToken":"refresh-current","expiresAt":1700000000000,"chatgptAccountId":"chat-b"},
			"lastCodexValidatedAt":1699999999000,
			"lastCodexValidationStatus":"failed",
			"lastCodexValidationError":"safe validation summary"
		}
	}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	snapshot := store.Read()
	if snapshot.Status != ManagedCredentialStoreOK {
		t.Fatalf("status=%q", snapshot.Status)
	}
	legacy := snapshot.Records["legacy"]
	if legacy.RefreshGrantFingerprint == "" || legacy.RefreshGrantFingerprint != RefreshGrantFingerprintForToken("refresh-legacy") {
		t.Fatalf("legacy fingerprint=%q", legacy.RefreshGrantFingerprint)
	}
	current := snapshot.Records["current"]
	if current.RefreshGrantFingerprint != RefreshGrantFingerprintForToken("refresh-current") {
		t.Fatalf("current fingerprint=%q", current.RefreshGrantFingerprint)
	}
	if current.LastValidatedAtMS == nil || *current.LastValidatedAtMS != 1699999999000 || current.LastValidationStatus != "failed" || current.LastValidationError != "safe validation summary" {
		t.Fatalf("validation metadata=%#v", current)
	}
}

func TestManagedCredentialStoreCompareAndSwapPreservesLineageAndMetadata(t *testing.T) {
	home := t.TempDir()
	replacedAt := int64(1699999998000)
	writeManagedStore(t, home, []byte(`{
		"acct": {
			"generation":7,
			"credential":{"accessToken":"old","refreshToken":"same-refresh","expiresAt":1700000000000,"chatgptAccountId":"chat-old"},
			"refreshGrantFingerprint":"existing-fingerprint",
			"replacedAt":1699999998000,
			"lastCodexValidatedAt":1699999999000,
			"lastCodexValidationStatus":"ok"
		}
	}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	newGeneration, err := store.CompareAndSwap(context.Background(), "acct", 7, ManagedCredential{
		AccessToken: "new", RefreshToken: "same-refresh", ExpiresAtMS: 1700003600000, ChatGPTAccountID: "chat-new",
	})
	if err != nil {
		t.Fatalf("CompareAndSwap(): %v", err)
	}
	if newGeneration != 8 {
		t.Fatalf("generation=%d", newGeneration)
	}
	got := store.Read().Records["acct"]
	if got.Generation != 8 || got.Credential == nil || got.Credential.AccessToken != "new" || got.Credential.ChatGPTAccountID != "chat-new" {
		t.Fatalf("record=%#v", got)
	}
	if got.RefreshGrantFingerprint != "existing-fingerprint" {
		t.Fatalf("unchanged refresh grant fingerprint=%q", got.RefreshGrantFingerprint)
	}
	if got.ReplacedAtMS == nil || *got.ReplacedAtMS != replacedAt {
		t.Fatalf("replacedAt=%v", got.ReplacedAtMS)
	}
	if got.LastValidatedAtMS == nil || *got.LastValidatedAtMS != 1699999999000 || got.LastValidationStatus != "ok" {
		t.Fatalf("validation metadata lost: %#v", got)
	}
}

func TestManagedCredentialStoreCompareAndSwapRecomputesFingerprintWhenRefreshGrantChanges(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":2,"credential":{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	if _, err := store.CompareAndSwap(context.Background(), "acct", 2, ManagedCredential{
		AccessToken: "new", RefreshToken: "new-refresh", ExpiresAtMS: 2, ChatGPTAccountID: "chat",
	}); err != nil {
		t.Fatal(err)
	}
	got := store.Read().Records["acct"]
	if got.RefreshGrantFingerprint != RefreshGrantFingerprintForToken("new-refresh") {
		t.Fatalf("fingerprint=%q", got.RefreshGrantFingerprint)
	}
	if got.ReplacedAtMS != nil {
		t.Fatalf("refresh CAS invented replacement timestamp=%v", got.ReplacedAtMS)
	}
}

func TestManagedCredentialStoreCompareAndSwapRejectsMissingStaleAndTombstonedRecords(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{
		"live":{"generation":3,"credential":{"accessToken":"a","refreshToken":"r","expiresAt":1,"chatgptAccountId":"c"}},
		"deleted":{"generation":4,"deletedAt":2}
	}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	credential := ManagedCredential{AccessToken: "new", RefreshToken: "r", ExpiresAtMS: 2, ChatGPTAccountID: "c"}
	for _, tc := range []struct {
		id         string
		generation int64
	}{
		{id: "missing", generation: 0},
		{id: "live", generation: 2},
		{id: "live", generation: 4},
		{id: "deleted", generation: 4},
	} {
		if _, err := store.CompareAndSwap(context.Background(), tc.id, tc.generation, credential); !errors.Is(err, ErrManagedCredentialGenerationConflict) {
			t.Fatalf("id=%q gen=%d err=%v", tc.id, tc.generation, err)
		}
	}
	if got := store.Read().Records["live"].Generation; got != 3 {
		t.Fatalf("failed CAS mutated generation=%d", got)
	}
}

func TestManagedCredentialStoreCompareAndSwapFailsClosedOnInvalidStore(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":`), 0o600)
	store := mustManagedCredentialStore(t, home)
	_, err := store.CompareAndSwap(context.Background(), "acct", 1, ManagedCredential{})
	if !errors.Is(err, ErrManagedCredentialStoreInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestManagedCredentialStoreConcurrentCASAllowsExactlyOneWriter(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":5,"credential":{"accessToken":"old","refreshToken":"r","expiresAt":1,"chatgptAccountId":"c"}}}`), 0o600)
	storeA := mustManagedCredentialStore(t, home)
	storeB := mustManagedCredentialStore(t, home)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for index, store := range []*ManagedCredentialStore{storeA, storeB} {
		wg.Add(1)
		go func(index int, store *ManagedCredentialStore) {
			defer wg.Done()
			<-start
			_, err := store.CompareAndSwap(context.Background(), "acct", 5, ManagedCredential{
				AccessToken: "writer-" + string(rune('A'+index)), RefreshToken: "r", ExpiresAtMS: 2, ChatGPTAccountID: "c",
			})
			results <- err
		}(index, store)
	}
	close(start)
	wg.Wait()
	close(results)

	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrManagedCredentialGenerationConflict):
			conflicts++
		default:
			t.Fatalf("unexpected CAS error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	if got := storeA.Read().Records["acct"].Generation; got != 6 {
		t.Fatalf("final generation=%d", got)
	}
}

func TestManagedCredentialStoreMutationLockHonorsCancellation(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"a","refreshToken":"r","expiresAt":1,"chatgptAccountId":"c"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	lock, err := acquireManagedStoreMutationLock(context.Background(), store.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	_, err = store.CompareAndSwap(ctx, "acct", 1, ManagedCredential{AccessToken: "b", RefreshToken: "r", ExpiresAtMS: 2, ChatGPTAccountID: "c"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if got := store.Read().Records["acct"].Generation; got != 1 {
		t.Fatalf("cancelled mutation changed generation=%d", got)
	}
}

func TestManagedCredentialStoreCASPersistsOwnerOnlyRegularJSON(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"a","refreshToken":"r","expiresAt":1,"chatgptAccountId":"c"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	if _, err := store.CompareAndSwap(context.Background(), "acct", 1, ManagedCredential{AccessToken: "b", RefreshToken: "r", ExpiresAtMS: 2, ChatGPTAccountID: "c"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "codex-accounts.json")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("mode=%v", info.Mode())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions=%o", info.Mode().Perm())
	}
	if got := store.Read(); got.Status != ManagedCredentialStoreOK || got.Records["acct"].Credential == nil || got.Records["acct"].Credential.AccessToken != "b" {
		t.Fatalf("reloaded=%#v", got)
	}
}
