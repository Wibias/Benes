package fabric

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// ArtifactStore is content-addressed by sha256 of the bytes.
type ArtifactStore struct{ Dir string }

func (a *ArtifactStore) Store(content []byte) (string, error) {
	sum := sha256.Sum256(content)
	h := hex.EncodeToString(sum[:])
	sub := filepath.Join(a.Dir, "sha256", h[:2], h)
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(sub); err == nil {
		return h, nil
	}
	return h, os.WriteFile(sub, content, 0o600)
}

func (a *ArtifactStore) Get(h string) ([]byte, error) {
	if len(h) != 64 {
		return nil, os.ErrNotExist
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, os.ErrNotExist
		}
	}
	return os.ReadFile(filepath.Join(a.Dir, "sha256", h[:2], h))
}
