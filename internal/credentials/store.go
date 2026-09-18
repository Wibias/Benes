package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var (
	ErrCredentialUnavailable = errors.New("credential is unavailable")
	ErrPlaintextDisabled     = errors.New("plaintext credential fallback is disabled")
)

type Source string

const (
	SourceSecureStore Source = "secure-store"
	SourceEnv         Source = "env"
	SourcePlaintext   Source = "plaintext"
)

type Ref struct {
	ID     string `json:"id"`
	Source Source `json:"source"`
	Hint   string `json:"hint,omitempty"`
}

type Record struct {
	Ref    Ref
	Secret []byte
}

type Store interface {
	Put(id string, secret []byte) (Ref, error)
	Get(ref Ref) ([]byte, error)
	Delete(ref Ref) error
}

type FileStore struct {
	dir            string
	allowPlaintext bool
	mu             sync.Mutex
}

func NewFileStore(dir string, allowPlaintext bool) (*FileStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("credential store directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create credential store: %w", err)
	}
	return &FileStore{dir: dir, allowPlaintext: allowPlaintext}, nil
}

func validFileCredentialID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > 255 {
		return false
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return false
	}
	return true
}

func (s *FileStore) Put(id string, secret []byte) (Ref, error) {
	if s == nil {
		return Ref{}, ErrCredentialUnavailable
	}
	id = strings.TrimSpace(id)
	if id != "" && !validFileCredentialID(id) {
		return Ref{}, ErrCredentialUnavailable
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, id+".json")
	body, err := json.Marshal(struct {
		ID     string `json:"id"`
		Secret []byte `json:"secret"`
	}{ID: id, Secret: append([]byte(nil), secret...)})
	if err != nil {
		return Ref{}, err
	}
	if err := atomicfile.Write(path, body, atomicfile.Options{Mode: 0o600}); err != nil {
		return Ref{}, fmt.Errorf("persist credential: %w", err)
	}
	return Ref{ID: id, Source: SourceSecureStore}, nil
}

func (s *FileStore) Get(ref Ref) ([]byte, error) {
	if s == nil {
		return nil, ErrCredentialUnavailable
	}
	switch ref.Source {
	case SourceEnv:
		value := strings.TrimSpace(os.Getenv(ref.Hint))
		if value == "" {
			return nil, ErrCredentialUnavailable
		}
		return []byte(value), nil
	case SourcePlaintext:
		if !s.allowPlaintext {
			return nil, ErrPlaintextDisabled
		}
		if len(ref.Hint) == 0 {
			return nil, ErrCredentialUnavailable
		}
		return []byte(ref.Hint), nil
	case SourceSecureStore, "":
		id := strings.TrimSpace(ref.ID)
		if !validFileCredentialID(id) {
			return nil, ErrCredentialUnavailable
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		data, err := atomicfile.ReadBounded(filepath.Join(s.dir, id+".json"), 1<<20)
		if err != nil {
			return nil, ErrCredentialUnavailable
		}
		var stored struct {
			Secret []byte `json:"secret"`
		}
		if json.Unmarshal(data, &stored) != nil || len(stored.Secret) == 0 {
			return nil, ErrCredentialUnavailable
		}
		return append([]byte(nil), stored.Secret...), nil
	default:
		return nil, ErrCredentialUnavailable
	}
}

func (s *FileStore) Delete(ref Ref) error {
	if s == nil || ref.Source != SourceSecureStore {
		return nil
	}
	id := strings.TrimSpace(ref.ID)
	if id == "" {
		return nil
	}
	if !validFileCredentialID(id) {
		return ErrCredentialUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *FileStore) List() ([]Ref, error) {
	if s == nil {
		return nil, ErrCredentialUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Ref, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if id == "" {
			continue
		}
		out = append(out, Ref{ID: id, Source: SourceSecureStore})
	}
	return out, nil
}
