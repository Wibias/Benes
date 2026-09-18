package managedfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var (
	ErrForeignHome      = errors.New("managed client home is not Benes-owned")
	ErrIndeterminate    = errors.New("managed client home ownership is indeterminate")
	ErrNewerUnsupported = errors.New("managed client metadata version is newer than this coordinator")
	ErrDisabled         = errors.New("managed client integration is disabled")
	ErrSymlink          = errors.New("managed artifact is a symlink")
	ErrConflict         = errors.New("managed artifact changed during the transaction")
)

const (
	currentVersion = 1
	metadataName   = "benes-managed.json"
	lockName       = "benes-managed.lock"
	journalName    = "benes-managed.journal"
	historyName    = "benes-managed.history"
)

var lockStaleAfter = 2 * time.Minute

type Ownership string

const (
	OwnershipNone          Ownership = "none"
	OwnershipCurrent       Ownership = "benes-current"
	OwnershipLegacy        Ownership = "benes-legacy"
	OwnershipUser          Ownership = "user"
	OwnershipForeign       Ownership = "foreign"
	OwnershipIndeterminate Ownership = "indeterminate"
	OwnershipDisabled      Ownership = "disabled"
	OwnershipDrifted       Ownership = "drifted"
)

type Record struct {
	Version    int               `json:"version"`
	Generation int64             `json:"generation"`
	State      string            `json:"state"`
	Client     string            `json:"client"`
	Digests    map[string]string `json:"digests,omitempty"`
}

type Coordinator struct {
	home string
}

func New(home string) (*Coordinator, error) {
	if home == "" {
		return nil, fmt.Errorf("managed client home is required")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	return &Coordinator{home: home}, nil
}

func (c *Coordinator) Classify() Ownership {
	data, err := os.ReadFile(c.metaPath())
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(c.home, "config.toml")); err == nil {
			return OwnershipUser
		}
		return OwnershipNone
	}
	if err != nil {
		return OwnershipIndeterminate
	}
	var rec Record
	if json.Unmarshal(data, &rec) != nil || rec.Client == "" {
		return OwnershipIndeterminate
	}
	if rec.Version > currentVersion {
		return OwnershipIndeterminate
	}
	switch rec.State {
	case "committed":
		if artifactDrifted(c.home, rec.Digests) {
			return OwnershipDrifted
		}
		return OwnershipCurrent
	case "adoption-pending", "legacy":
		return OwnershipLegacy
	case "foreign":
		return OwnershipForeign
	case "off", "disabled":
		return OwnershipDisabled
	default:
		return OwnershipIndeterminate
	}
}

type stagedFile struct {
	name     string
	data     []byte
	expected []byte
	existed  bool
}

type Txn struct {
	coord  *Coordinator
	lock   *os.File
	next   Record
	staged []stagedFile
}

func (c *Coordinator) Enable(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := acquireLock(ctx, filepath.Join(c.home, lockName))
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lock.Name())
	}()
	own := c.Classify()
	switch own {
	case OwnershipCurrent:
		return nil
	case OwnershipDisabled:
	default:
		return ErrIndeterminate
	}
	rec, err := readRecord(c.metaPath())
	if err != nil {
		return err
	}
	rec.State = "committed"
	return writeJSON(c.metaPath(), rec)
}

func (c *Coordinator) Recover(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := acquireLock(ctx, filepath.Join(c.home, lockName))
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lock.Name())
	}()
	journal := filepath.Join(c.home, journalName)
	if _, err := os.Stat(journal); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.Remove(journal)
}

func (c *Coordinator) Disable(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := acquireLock(ctx, filepath.Join(c.home, lockName))
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lock.Name())
	}()
	own := c.Classify()
	switch own {
	case OwnershipDisabled:
		return nil
	case OwnershipCurrent, OwnershipLegacy:
	default:
		return ErrIndeterminate
	}
	rec, err := readRecord(c.metaPath())
	if err != nil {
		return err
	}
	rec.State = "off"
	if err := writeJSON(c.metaPath(), rec); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(c.home, journalName))
	return nil
}

func (c *Coordinator) Adopt(ctx context.Context, client string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := acquireLock(ctx, filepath.Join(c.home, lockName))
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lock.Name())
	}()
	own := c.Classify()
	switch own {
	case OwnershipCurrent:
		return nil
	case OwnershipLegacy:
	default:
		if own == OwnershipUser || own == OwnershipForeign {
			return ErrForeignHome
		}
		return ErrIndeterminate
	}
	rec, err := readRecord(c.metaPath())
	if err != nil {
		return ErrIndeterminate
	}
	if rec.Version > currentVersion {
		return ErrNewerUnsupported
	}
	if client != "" {
		rec.Client = client
	}
	if rec.Client == "" {
		return ErrIndeterminate
	}
	rec.Version = currentVersion
	rec.State = "adoption-pending"
	if err := writeJSON(filepath.Join(c.home, journalName), rec); err != nil {
		return err
	}
	rec.State = "committed"
	if err := writeJSON(c.metaPath(), rec); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(c.home, journalName))
	return nil
}

func (c *Coordinator) Restore(ctx context.Context, generation int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if generation < 1 {
		return fmt.Errorf("history generation is required")
	}
	root := filepath.Join(c.home, historyName, strconv.FormatInt(generation, 10))
	files := map[string][]byte{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("history generation %d is empty", generation)
	}
	tx, err := c.begin(ctx, "", true)
	if err != nil {
		return err
	}
	for name, data := range files {
		if err := tx.Stage(name, data); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (c *Coordinator) Begin(ctx context.Context, client string) (*Txn, error) {
	return c.begin(ctx, client, false)
}

func (c *Coordinator) BeginReassert(ctx context.Context, client string) (*Txn, error) {
	return c.begin(ctx, client, true)
}

func (c *Coordinator) begin(ctx context.Context, client string, allowDrift bool) (*Txn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lock, err := acquireLock(ctx, filepath.Join(c.home, lockName))
	if err != nil {
		return nil, err
	}
	own := c.Classify()
	switch own {
	case OwnershipForeign:
		_ = lock.Close()
		return nil, ErrForeignHome
	case OwnershipDrifted:
		if !allowDrift {
			_ = lock.Close()
			return nil, ErrForeignHome
		}
	case OwnershipIndeterminate:
		_ = lock.Close()
		return nil, ErrIndeterminate
	case OwnershipDisabled:
		_ = lock.Close()
		return nil, ErrDisabled
	}
	rec := Record{Version: currentVersion, Client: client, State: "pending", Generation: 1}
	if existing, err := readRecord(c.metaPath()); err == nil {
		if existing.Version > currentVersion {
			_ = lock.Close()
			return nil, ErrNewerUnsupported
		}
		rec.Generation = existing.Generation + 1
		if rec.Client == "" {
			rec.Client = existing.Client
		}
		rec.Digests = cloneDigests(existing.Digests)
	}
	if err := writeJSON(filepath.Join(c.home, journalName), rec); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &Txn{coord: c, lock: lock, next: rec}, nil
}

func (t *Txn) Stage(name string, data []byte) error {
	if t == nil {
		return fmt.Errorf("transaction is required")
	}
	clean, err := sanitizeArtifactName(name)
	if err != nil {
		return err
	}
	dest := filepath.Join(t.coord.home, clean)
	if isSymlink(dest) {
		return ErrSymlink
	}
	current, err := os.ReadFile(dest)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	t.staged = append(t.staged, stagedFile{
		name:     clean,
		data:     append([]byte(nil), data...),
		expected: append([]byte(nil), current...),
		existed:  existed,
	})
	return nil
}

func (t *Txn) Commit() error {
	if t == nil {
		return nil
	}
	defer t.release()
	for _, file := range t.staged {
		dest := filepath.Join(t.coord.home, file.name)
		if isSymlink(dest) {
			return ErrSymlink
		}
		current, err := os.ReadFile(dest)
		if file.existed {
			if err != nil || !bytes.Equal(current, file.expected) {
				return ErrConflict
			}
			if bytes.Equal(current, file.data) {
				continue
			}
			if err := snapshotHistory(t.coord.home, t.next.Generation-1, file.name, file.expected); err != nil {
				return err
			}
		} else if err == nil {
			return ErrConflict
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := atomicfile.Write(dest, file.data, atomicfile.Options{Mode: 0o600}); err != nil {
			return err
		}
	}
	t.next.State = "committed"
	if t.next.Digests == nil {
		t.next.Digests = map[string]string{}
	}
	for _, file := range t.staged {
		t.next.Digests[file.name] = digestHex(file.data)
	}
	if err := writeJSON(t.coord.metaPath(), t.next); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(t.coord.home, journalName))
	return nil
}

func sanitizeArtifactName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("invalid managed artifact name")
	}
	cleaned := filepath.ToSlash(name)
	if strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid managed artifact name")
	}
	return filepath.FromSlash(cleaned), nil
}

func (t *Txn) Rollback() error {
	if t == nil {
		return nil
	}
	defer t.release()
	_ = os.Remove(filepath.Join(t.coord.home, journalName))
	return nil
}

func (t *Txn) release() {
	if t.lock != nil {
		_ = t.lock.Close()
		_ = os.Remove(t.lock.Name())
		t.lock = nil
	}
}

func (c *Coordinator) metaPath() string {
	return filepath.Join(c.home, metadataName)
}

func acquireLock(ctx context.Context, path string) (*os.File, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && lockStaleAfter > 0 && time.Since(info.ModTime()) > lockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func readRecord(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if json.Unmarshal(data, &rec) != nil {
		return Record{}, ErrIndeterminate
	}
	return rec, nil
}

func artifactDrifted(home string, digests map[string]string) bool {
	if len(digests) == 0 {
		return false
	}
	for name, want := range digests {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || digestHex(data) != want {
			return true
		}
	}
	return false
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneDigests(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func snapshotHistory(home string, generation int64, name string, data []byte) error {
	if generation < 1 {
		return nil
	}
	dest := filepath.Join(home, historyName, strconv.FormatInt(generation, 10), name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(dest, data, atomicfile.Options{Mode: 0o600})
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

func writeJSON(path string, rec Record) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, body, atomicfile.Options{Mode: 0o600})
}
