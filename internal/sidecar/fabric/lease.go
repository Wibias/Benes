package fabric

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const LeaseSchemaVersion = 1

var (
	ErrLeaseMalformed = errors.New("lease state malformed")
	ErrLeaseStale     = errors.New("stale fencing token")
	ErrLeaseFuture    = errors.New("future fencing token")
	ErrLeaseUnowned   = errors.New("lease owner mismatch")
	ErrLeaseHeld      = errors.New("write lease held")
	ErrLeaseEscape    = errors.New("lease path escapes fabric root")
)

// Leases models the per-task write lease and monotonic fencing token.
// Root is the fabric store directory; paths are confined under Root/leases.
type Leases struct {
	Root   string
	TaskID string
}

type leaseState struct {
	Version      int    `json:"version"`
	FencingToken int    `json:"fencing_token"`
	Owner        string `json:"owner"`
}

func (l *Leases) path() (string, error) {
	if l == nil {
		return "", ErrLeaseEscape
	}
	if err := validTaskID(l.TaskID); err != nil {
		return "", InvalidTask("invalid task id")
	}
	root := filepath.Clean(l.Root)
	if root == "" || root == "." {
		return "", ErrLeaseEscape
	}
	leasesDir := filepath.Join(root, "leases")
	name := l.TaskID + ".lease.json"
	if name != filepath.Base(name) || strings.Contains(name, "..") {
		return "", ErrLeaseEscape
	}
	full := filepath.Join(leasesDir, name)
	clean := filepath.Clean(full)
	sep := string(os.PathSeparator)
	if clean != leasesDir && !strings.HasPrefix(clean, leasesDir+sep) {
		return "", ErrLeaseEscape
	}
	if info, err := os.Lstat(clean); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: lease file is a symlink", ErrLeaseEscape)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if info, err := os.Lstat(leasesDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: leases directory is a symlink", ErrLeaseEscape)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return clean, nil
}

func (l *Leases) read() (leaseState, error) {
	path, err := l.path()
	if err != nil {
		return leaseState{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return leaseState{Version: LeaseSchemaVersion}, nil
		}
		return leaseState{}, err
	}
	if len(b) == 0 {
		return leaseState{}, fmt.Errorf("%w: empty", ErrLeaseMalformed)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return leaseState{}, fmt.Errorf("%w: %v", ErrLeaseMalformed, err)
	}
	// Fail closed on truncated or unknown secret-bearing fields.
	for k := range raw {
		lk := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
		if _, denied := deniedPayloadKeys[lk]; denied {
			return leaseState{}, fmt.Errorf("%w: forbidden field %q", ErrLeaseMalformed, k)
		}
		if lk == "secret" || lk == "token" || lk == "api_key" || lk == "prompt" {
			return leaseState{}, fmt.Errorf("%w: forbidden field %q", ErrLeaseMalformed, k)
		}
	}
	var s leaseState
	if err := json.Unmarshal(b, &s); err != nil {
		return leaseState{}, fmt.Errorf("%w: %v", ErrLeaseMalformed, err)
	}
	if s.Version == 0 {
		// Legacy v0 files (no version) are accepted once and rewritten on next write.
		s.Version = LeaseSchemaVersion
	} else if s.Version != LeaseSchemaVersion {
		return leaseState{}, fmt.Errorf("%w: unsupported version %d", ErrLeaseMalformed, s.Version)
	}
	if s.FencingToken < 0 {
		return leaseState{}, fmt.Errorf("%w: negative fencing token", ErrLeaseMalformed)
	}
	if s.Owner != "" {
		if _, err := validateIdentity(s.Owner, "owner"); err != nil {
			return leaseState{}, fmt.Errorf("%w: %v", ErrLeaseMalformed, err)
		}
	}
	return s, nil
}

// leaseAtomicWriteHook, when non-nil, is invoked instead of atomicfile.Write
// (tests inject failures). Production keeps this nil.
var leaseAtomicWriteHook func(path string, content []byte) error

func (l *Leases) atomicWrite(s leaseState) error {
	path, err := l.path()
	if err != nil {
		return err
	}
	s.Version = LeaseSchemaVersion
	if s.FencingToken < 0 {
		return fmt.Errorf("%w: negative fencing token", ErrLeaseMalformed)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// atomicfile replaces without a remove-old-before-new window.
	if leaseAtomicWriteHook != nil {
		return leaseAtomicWriteHook(path, b)
	}
	return atomicfile.Write(path, b, atomicfile.Options{Mode: 0o600})
}

func (l *Leases) Token() (int, error) {
	s, err := l.read()
	if err != nil {
		return 0, err
	}
	return s.FencingToken, nil
}

func (l *Leases) Owner() (string, error) {
	s, err := l.read()
	if err != nil {
		return "", err
	}
	return s.Owner, nil
}

// AcquireWriteLease grants the lease if free or already owned by owner.
func (l *Leases) AcquireWriteLease(owner string) (int, error) {
	owner, err := validateIdentity(owner, "owner")
	if err != nil {
		return 0, err
	}
	if owner == "" {
		return 0, InvalidTask("owner is required")
	}
	s, err := l.read()
	if err != nil {
		return 0, err
	}
	if s.Owner != "" && s.Owner != owner {
		return 0, fmt.Errorf("%w by %s", ErrLeaseHeld, s.Owner)
	}
	s.Owner = owner
	if err := l.atomicWrite(s); err != nil {
		return 0, err
	}
	return s.FencingToken, nil
}

// CheckFencing rejects stale writers whose token is behind the current fence.
// Prefer CheckOwnerAndFence for execution terminals.
func (l *Leases) CheckFencing(token int) error {
	s, err := l.read()
	if err != nil {
		return err
	}
	if token < s.FencingToken {
		return fmt.Errorf("%w: token %d < current %d", ErrLeaseStale, token, s.FencingToken)
	}
	if token > s.FencingToken {
		return fmt.Errorf("%w: token %d > current %d", ErrLeaseFuture, token, s.FencingToken)
	}
	return nil
}

// CheckOwnerAndFence requires an exact owner and fencing token match.
// Stale, future, and unowned claims are all rejected (fail closed).
func (l *Leases) CheckOwnerAndFence(owner string, token int) error {
	s, err := l.read()
	if err != nil {
		return err
	}
	if owner != s.Owner {
		return fmt.Errorf("%w: expected %q got %q", ErrLeaseUnowned, s.Owner, owner)
	}
	if token < s.FencingToken {
		return fmt.Errorf("%w: token %d < current %d", ErrLeaseStale, token, s.FencingToken)
	}
	if token > s.FencingToken {
		return fmt.Errorf("%w: token %d > current %d", ErrLeaseFuture, token, s.FencingToken)
	}
	return nil
}

// CommitHandoffFrom is a compare-and-swap handoff: the lease must currently be
// owned by expectedOwner at expectedFence before flipping to newOwner and
// incrementing the fencing token (N -> N+1).
func (l *Leases) CommitHandoffFrom(expectedOwner string, expectedFence int, newOwner string) (int, error) {
	expectedOwner, err := validateIdentity(expectedOwner, "owner")
	if err != nil {
		return 0, err
	}
	if expectedOwner == "" {
		return 0, InvalidTask("owner is required")
	}
	newOwner, err = validateIdentity(newOwner, "owner")
	if err != nil {
		return 0, err
	}
	if newOwner == "" {
		return 0, InvalidTask("owner is required")
	}
	s, err := l.read()
	if err != nil {
		return 0, err
	}
	if s.Owner != expectedOwner {
		return 0, fmt.Errorf("%w: expected %q got %q", ErrLeaseUnowned, expectedOwner, s.Owner)
	}
	if expectedFence < s.FencingToken {
		return 0, fmt.Errorf("%w: token %d < current %d", ErrLeaseStale, expectedFence, s.FencingToken)
	}
	if expectedFence > s.FencingToken {
		return 0, fmt.Errorf("%w: token %d > current %d", ErrLeaseFuture, expectedFence, s.FencingToken)
	}
	s.Owner = newOwner
	s.FencingToken++
	if err := l.atomicWrite(s); err != nil {
		return 0, err
	}
	return s.FencingToken, nil
}

// CommitHandoff flips the owner and increments the fencing token (one-way door).
func (l *Leases) CommitHandoff(newOwner string) (int, error) {
	newOwner, err := validateIdentity(newOwner, "owner")
	if err != nil {
		return 0, err
	}
	if newOwner == "" {
		return 0, InvalidTask("owner is required")
	}
	s, err := l.read()
	if err != nil {
		return 0, err
	}
	s.Owner = newOwner
	s.FencingToken++
	if err := l.atomicWrite(s); err != nil {
		return 0, err
	}
	return s.FencingToken, nil
}

// Release bumps the fencing token and clears the owner so a later run cannot
// complete with a stale claim.
func (l *Leases) Release() (int, error) {
	s, err := l.read()
	if err != nil {
		return 0, err
	}
	s.Owner = ""
	s.FencingToken++
	if err := l.atomicWrite(s); err != nil {
		return 0, err
	}
	return s.FencingToken, nil
}
