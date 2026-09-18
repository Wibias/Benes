package credentials

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memBackend struct {
	mu      sync.Mutex
	items   map[string][]byte
	failPut bool
	avail   error
}

func (m *memBackend) Available() error { return m.avail }

func (m *memBackend) Put(id string, secret []byte) error {
	if m.avail != nil {
		return m.avail
	}
	if m.failPut {
		return errors.New("backend write failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = map[string][]byte{}
	}
	m.items[id] = append([]byte(nil), secret...)
	return nil
}

func (m *memBackend) Get(id string) ([]byte, error) {
	if m.avail != nil {
		return nil, m.avail
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	secret, ok := m.items[id]
	if !ok {
		return nil, ErrCredentialUnavailable
	}
	return append([]byte(nil), secret...), nil
}

func (m *memBackend) Delete(id string) error {
	if m.avail != nil {
		return m.avail
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id)
	return nil
}

func TestOSStoreRoundTripWithoutLeakingSecrets(t *testing.T) {
	store := NewOSStore(&memBackend{})
	ref, err := store.Put("slot-a", []byte("sk-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Source != SourceSecureStore || ref.ID != "slot-a" {
		t.Fatalf("ref=%#v", ref)
	}
	got, err := store.Get(ref)
	if err != nil || string(got) != "sk-secret" {
		t.Fatalf("get=%q %v", got, err)
	}
	if err := store.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ref); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("after delete=%v", err)
	}
}

func TestOSStoreFailsClosedWhenBackendUnavailable(t *testing.T) {
	store := NewOSStore(&memBackend{avail: ErrSecureStoreUnavailable})
	_, err := store.Put("slot-a", []byte("sk-secret"))
	if !errors.Is(err, ErrSecureStoreUnavailable) || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("put=%v", err)
	}
	_, err = store.Get(Ref{ID: "slot-a", Source: SourceSecureStore})
	if !errors.Is(err, ErrSecureStoreUnavailable) {
		t.Fatalf("get=%v", err)
	}
}

func TestOSStoreDoesNotChangeRefOnFailedWrite(t *testing.T) {
	backend := &memBackend{failPut: true}
	store := NewOSStore(backend)
	_, err := store.Put("slot-a", []byte("sk-secret"))
	if err == nil {
		t.Fatal("expected write failure")
	}
	if _, err := store.Get(Ref{ID: "slot-a", Source: SourceSecureStore}); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("partial write escaped: %v", err)
	}
}

func TestOSStoreRejectsEmptySecret(t *testing.T) {
	store := NewOSStore(&memBackend{})
	_, err := store.Put("slot-a", nil)
	if !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestDefaultBackendReportsHeadlessUnavailability(t *testing.T) {
	err := defaultBackend().Available()
	if err != nil && !errors.Is(err, ErrSecureStoreUnavailable) {
		if strings.Contains(err.Error(), "sk-") {
			t.Fatalf("leaked=%v", err)
		}
	}
}

func TestWindowsCredentialManagerRoundTrip(t *testing.T) {
	backend := defaultBackend()
	if err := backend.Available(); err != nil {
		t.Skip(err)
	}
	id := "test-" + t.Name()
	store := NewOSStore(backend)
	ref, err := store.Put(id, []byte("sk-windows-live"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(ref) })
	got, err := readSecretUntilPresent(func() ([]byte, error) {
		return store.Get(ref)
	}, []byte("sk-windows-live"), secureStoreReadAttempts, secureStoreSleep)
	if err != nil || string(got) != "sk-windows-live" {
		t.Fatalf("get=%q %v", got, err)
	}
}

func TestReadSecretUntilPresentRetriesUnavailableThenMatches(t *testing.T) {
	n := 0
	sleeps := 0
	got, err := readSecretUntilPresent(func() ([]byte, error) {
		n++
		if n < 3 {
			return nil, ErrCredentialUnavailable
		}
		return []byte("sk-windows-live"), nil
	}, []byte("sk-windows-live"), 5, func(time.Duration) { sleeps++ })
	if err != nil || string(got) != "sk-windows-live" || n != 3 || sleeps != 2 {
		t.Fatalf("err=%v n=%d sleeps=%d", err, n, sleeps)
	}
}

func TestReadSecretUntilPresentStopsOnPersistentUnavailable(t *testing.T) {
	n := 0
	got, err := readSecretUntilPresent(func() ([]byte, error) {
		n++
		return nil, ErrCredentialUnavailable
	}, []byte("sk-windows-live"), 3, func(time.Duration) {})
	if !errors.Is(err, ErrCredentialUnavailable) || len(got) != 0 || n != 3 {
		t.Fatalf("err=%v n=%d gotlen=%d", err, n, len(got))
	}
}

func TestReadSecretUntilPresentDoesNotRetrySecureStoreUnavailable(t *testing.T) {
	n := 0
	_, err := readSecretUntilPresent(func() ([]byte, error) {
		n++
		return nil, ErrSecureStoreUnavailable
	}, []byte("sk-windows-live"), 5, func(time.Duration) { t.Fatal("slept") })
	if !errors.Is(err, ErrSecureStoreUnavailable) || n != 1 {
		t.Fatalf("err=%v n=%d", err, n)
	}
}

func TestOSStorePutRetriesTransientGetUnavailable(t *testing.T) {
	backend := &flakyGetBackend{remaining: 2}
	origSleep := secureStoreSleep
	secureStoreSleep = func(time.Duration) {}
	t.Cleanup(func() { secureStoreSleep = origSleep })
	store := NewOSStore(backend)
	ref, err := store.Put("slot-retry", []byte("sk-retry"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ref)
	if err != nil || string(got) != "sk-retry" {
		t.Fatalf("get=%q %v", got, err)
	}
}

type flakyGetBackend struct {
	memBackend
	remaining int
}

func (f *flakyGetBackend) Get(id string) ([]byte, error) {
	f.mu.Lock()
	if f.remaining > 0 {
		f.remaining--
		f.mu.Unlock()
		return nil, ErrCredentialUnavailable
	}
	f.mu.Unlock()
	return f.memBackend.Get(id)
}
