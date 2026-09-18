package credentials

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	secureStoreReadAttempts = 8
	secureStoreReadBackoff  = 25 * time.Millisecond
)

var secureStoreSleep = time.Sleep

var (
	ErrSecureStoreUnavailable = errors.New("secure credential store is unavailable")
)

type Backend interface {
	Available() error
	Put(id string, secret []byte) error
	Get(id string) ([]byte, error)
	Delete(id string) error
}

type OSStore struct {
	backend Backend
}

func NewOSStore(backend Backend) *OSStore {
	if backend == nil {
		backend = defaultBackend()
	}
	return &OSStore{backend: backend}
}

func (s *OSStore) Put(id string, secret []byte) (Ref, error) {
	if s == nil || s.backend == nil {
		return Ref{}, ErrSecureStoreUnavailable
	}
	if err := s.backend.Available(); err != nil {
		return Ref{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return Ref{}, fmt.Errorf("generate credential id: %w", err)
		}
		id = hex.EncodeToString(raw)
	}
	if len(secret) == 0 {
		return Ref{}, ErrCredentialUnavailable
	}
	if err := s.backend.Put(id, secret); err != nil {
		return Ref{}, fmt.Errorf("persist credential: %w", sanitizeStoreError(err))
	}
	got, err := readSecretUntilPresent(func() ([]byte, error) {
		return s.backend.Get(id)
	}, secret, secureStoreReadAttempts, secureStoreSleep)
	if err != nil || !bytes.Equal(got, secret) {
		_ = s.backend.Delete(id)
		return Ref{}, ErrCredentialUnavailable
	}
	return Ref{ID: id, Source: SourceSecureStore}, nil
}

func (s *OSStore) Get(ref Ref) ([]byte, error) {
	if s == nil || s.backend == nil {
		return nil, ErrSecureStoreUnavailable
	}
	if err := s.backend.Available(); err != nil {
		return nil, err
	}
	switch ref.Source {
	case SourceEnv:
		return (&FileStore{allowPlaintext: false}).Get(ref)
	case SourcePlaintext:
		return nil, ErrPlaintextDisabled
	case SourceSecureStore, "":
		if strings.TrimSpace(ref.ID) == "" {
			return nil, ErrCredentialUnavailable
		}
		secret, err := s.backend.Get(ref.ID)
		if err != nil {
			if errors.Is(err, ErrCredentialUnavailable) || errors.Is(err, ErrSecureStoreUnavailable) {
				return nil, err
			}
			return nil, ErrCredentialUnavailable
		}
		if len(secret) == 0 {
			return nil, ErrCredentialUnavailable
		}
		return append([]byte(nil), secret...), nil
	default:
		return nil, ErrCredentialUnavailable
	}
}

func (s *OSStore) Delete(ref Ref) error {
	if s == nil || s.backend == nil || ref.Source != SourceSecureStore || strings.TrimSpace(ref.ID) == "" {
		return nil
	}
	if err := s.backend.Available(); err != nil {
		return err
	}
	if err := s.backend.Delete(ref.ID); err != nil && !errors.Is(err, ErrCredentialUnavailable) {
		return err
	}
	return nil
}

func sanitizeStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrSecureStoreUnavailable) || errors.Is(err, ErrCredentialUnavailable) {
		return err
	}
	return ErrCredentialUnavailable
}

func readSecretUntilPresent(get func() ([]byte, error), want []byte, attempts int, sleep func(time.Duration)) ([]byte, error) {
	if attempts < 1 {
		attempts = 1
	}
	var last []byte
	var err error
	for i := 0; i < attempts; i++ {
		last, err = get()
		if err == nil && len(last) > 0 && (want == nil || bytes.Equal(last, want)) {
			return last, nil
		}
		if err != nil && !errors.Is(err, ErrCredentialUnavailable) {
			return nil, err
		}
		if i == attempts-1 {
			break
		}
		if sleep != nil {
			sleep(time.Duration(i+1) * secureStoreReadBackoff)
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, ErrCredentialUnavailable
}
