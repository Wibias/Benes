package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var (
	ErrConfigConflict      = errors.New("config transaction conflicts with a newer durable value")
	ErrConfigOwnership     = errors.New("config fragment is not owned by the requested owner")
	ErrConfigPath          = errors.New("invalid config mutation path")
	ErrConfigReservedPath  = errors.New("config mutation targets reserved Benes metadata")
	ErrConfigAlreadyCommit = errors.New("config transaction was already committed")
)

const managedMetadataKey = "_benesManaged"

type Revision string

type ConfigPath []string

func JSONPath(parts ...string) ConfigPath {
	return append(ConfigPath(nil), parts...)
}

type mutationKind uint8

const (
	mutationSet mutationKind = iota + 1
	mutationDelete
)

type configMutation struct {
	kind  mutationKind
	path  ConfigPath
	value json.RawMessage
	owner string
}

type configState struct {
	root     map[string]json.RawMessage
	raw      []byte
	source   DiskConfigSource
	revision Revision
}

type TransactionStore struct {
	path     string
	maxBytes int64
	commitMu *sync.Mutex
}

type Transaction struct {
	store     *TransactionStore
	baseline  configState
	mutations []configMutation
	source    MutationSource
	committed bool
}

var configCommitLocks sync.Map

func NewTransactionStore(path string, maxBytes int64) *TransactionStore {
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	cleaned := filepath.Clean(path)
	lockKey := cleaned
	if absolute, err := filepath.Abs(cleaned); err == nil {
		lockKey = absolute
	}
	lockValue, _ := configCommitLocks.LoadOrStore(lockKey, &sync.Mutex{})
	return &TransactionStore{
		path:     path,
		maxBytes: maxBytes,
		commitMu: lockValue.(*sync.Mutex),
	}
}

func (s *TransactionStore) Begin() (*Transaction, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil, fmt.Errorf("config path is required")
	}
	state, err := s.readState()
	if err != nil {
		return nil, err
	}
	return &Transaction{store: s, baseline: state}, nil
}

func (tx *Transaction) BaselineRevision() Revision {
	if tx == nil {
		return ""
	}
	return tx.baseline.revision
}

func (tx *Transaction) Set(path ConfigPath, value json.RawMessage) error {
	return tx.queue(configMutation{kind: mutationSet, path: path, value: value})
}

func (tx *Transaction) Delete(path ConfigPath) error {
	return tx.queue(configMutation{kind: mutationDelete, path: path})
}

func (tx *Transaction) SetOwned(path ConfigPath, value json.RawMessage, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("%w: owner is required", ErrConfigOwnership)
	}
	return tx.queue(configMutation{kind: mutationSet, path: path, value: value, owner: owner})
}

func (tx *Transaction) DeleteOwned(path ConfigPath, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("%w: owner is required", ErrConfigOwnership)
	}
	return tx.queue(configMutation{kind: mutationDelete, path: path, owner: owner})
}

func (tx *Transaction) queue(mutation configMutation) error {
	if tx == nil || tx.store == nil {
		return fmt.Errorf("config transaction is nil")
	}
	if tx.committed {
		return ErrConfigAlreadyCommit
	}
	path, err := normalizeMutationPath(mutation.path)
	if err != nil {
		return err
	}
	mutation.path = path
	if mutation.kind == mutationSet {
		if len(bytes.TrimSpace(mutation.value)) == 0 || !json.Valid(mutation.value) {
			return fmt.Errorf("%w: set value must be valid JSON", ErrConfigPath)
		}
		mutation.value = append(json.RawMessage(nil), mutation.value...)
	}
	tx.mutations = append(tx.mutations, mutation)
	return nil
}

func (tx *Transaction) Commit() (Revision, error) {
	if tx == nil || tx.store == nil {
		return "", fmt.Errorf("config transaction is nil")
	}
	if tx.committed {
		return "", ErrConfigAlreadyCommit
	}
	tx.store.commitMu.Lock()
	defer tx.store.commitMu.Unlock()

	current, err := tx.store.readState()
	if err != nil {
		return "", err
	}
	next := cloneRawObject(current.root)

	for _, mutation := range tx.mutations {
		baselineValue, baselineExists, baselineErr := lookupRaw(tx.baseline.root, mutation.path)
		currentValue, currentExists, currentErr := lookupRaw(current.root, mutation.path)
		if baselineErr != nil || currentErr != nil {
			return "", fmt.Errorf("%w at %s", ErrConfigConflict, formatPath(mutation.path))
		}
		if !sameJSONValue(baselineValue, baselineExists, currentValue, currentExists) {
			return "", fmt.Errorf("%w at %s", ErrConfigConflict, formatPath(mutation.path))
		}

		if mutation.owner != "" {
			owner, owned, err := managedOwner(current.root, mutation.path)
			if err != nil {
				return "", err
			}
			switch mutation.kind {
			case mutationSet:
				if owned && owner != mutation.owner {
					return "", fmt.Errorf("%w at %s: owned by %q", ErrConfigOwnership, formatPath(mutation.path), owner)
				}
			case mutationDelete:
				if !owned || owner != mutation.owner {
					return "", fmt.Errorf("%w at %s", ErrConfigOwnership, formatPath(mutation.path))
				}
			}
		}

		switch mutation.kind {
		case mutationSet:
			if err := setRaw(next, mutation.path, mutation.value); err != nil {
				return "", err
			}
			if mutation.owner != "" {
				if err := setManagedOwner(next, mutation.path, mutation.owner); err != nil {
					return "", err
				}
			}
		case mutationDelete:
			if err := deleteRaw(next, mutation.path); err != nil {
				return "", err
			}
			if mutation.owner != "" {
				if err := deleteManagedOwner(next, mutation.path); err != nil {
					return "", err
				}
			}
		default:
			return "", fmt.Errorf("unknown config mutation kind %d", mutation.kind)
		}
	}

	if err := scrubPersistedSecrets(next); err != nil {
		return "", fmt.Errorf("scrub persisted provider secrets: %w", err)
	}
	content, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode merged config: %w", err)
	}
	content = append(content, '\n')
	if int64(len(content)) > tx.store.maxBytes {
		return "", ErrConfigTooLarge
	}
	if _, err := decodeDiskConfigBytes(content, DiskConfigSourceFile); err != nil {
		return "", fmt.Errorf("validate merged config: %w", err)
	}
	if compactJSONEqual(current.root, next) {
		tx.committed = true
		return current.revision, nil
	}
	revision := revisionFor(DiskConfigSourceFile, content)
	if err := atomicfile.Write(tx.store.path, content, atomicfile.Options{Mode: 0o600}); err != nil {
		return "", fmt.Errorf("publish config transaction: %w", err)
	}
	if err := tx.persistAudit(next, revision); err != nil {
		return "", fmt.Errorf("config published but audit failed: %w", err)
	}

	tx.committed = true
	return revision, nil
}

func (s *TransactionStore) readState() (configState, error) {
	data, err := atomicfile.ReadBounded(s.path, s.maxBytes)
	source := DiskConfigSourceFile
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, atomicfile.ErrTooLarge) {
				return configState{}, ErrConfigTooLarge
			}
			return configState{}, fmt.Errorf("read config transaction baseline: %w", err)
		}
		data = []byte(defaultDiskConfigJSON)
		source = DiskConfigSourceDefault
	}
	decoded, err := decodeDiskConfigBytes(data, source)
	if err != nil {
		return configState{}, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(decoded.Raw, &root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("root must be an object")
		}
		return configState{}, fmt.Errorf("decode config transaction baseline: %w", err)
	}
	return configState{
		root:     root,
		raw:      append([]byte(nil), decoded.Raw...),
		source:   source,
		revision: revisionFor(source, decoded.Raw),
	}, nil
}

func revisionFor(source DiskConfigSource, raw []byte) Revision {
	hash := sha256.New()
	_, _ = hash.Write([]byte(source))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(raw)
	return Revision(hex.EncodeToString(hash.Sum(nil)))
}

func normalizeMutationPath(path ConfigPath) (ConfigPath, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("%w: path is empty", ErrConfigPath)
	}
	normalized := make(ConfigPath, len(path))
	for i, segment := range path {
		if segment == "" || strings.TrimSpace(segment) != segment {
			return nil, fmt.Errorf("%w: invalid segment %q", ErrConfigPath, segment)
		}
		normalized[i] = segment
	}
	if normalized[0] == managedMetadataKey {
		return nil, fmt.Errorf("%w: %s", ErrConfigReservedPath, managedMetadataKey)
	}
	return normalized, nil
}

func cloneRawObject(root map[string]json.RawMessage) map[string]json.RawMessage {
	clone := make(map[string]json.RawMessage, len(root))
	for key, value := range root {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func lookupRaw(root map[string]json.RawMessage, path ConfigPath) (json.RawMessage, bool, error) {
	current := root
	for index, segment := range path {
		raw, exists := current[segment]
		if !exists {
			return nil, false, nil
		}
		if index == len(path)-1 {
			return append(json.RawMessage(nil), raw...), true, nil
		}
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw, &child); err != nil || child == nil {
			return nil, false, fmt.Errorf("%w: parent %s is not an object", ErrConfigPath, formatPath(path[:index+1]))
		}
		current = child
	}
	return nil, false, nil
}

func setRaw(root map[string]json.RawMessage, path ConfigPath, value json.RawMessage) error {
	if len(path) == 1 {
		root[path[0]] = append(json.RawMessage(nil), value...)
		return nil
	}
	child := make(map[string]json.RawMessage)
	if raw, exists := root[path[0]]; exists {
		if err := json.Unmarshal(raw, &child); err != nil || child == nil {
			return fmt.Errorf("%w: parent %s is not an object", ErrConfigPath, formatPath(path[:1]))
		}
	}
	if err := setRaw(child, path[1:], value); err != nil {
		return err
	}
	encoded, err := json.Marshal(child)
	if err != nil {
		return fmt.Errorf("encode config path %s: %w", formatPath(path[:1]), err)
	}
	root[path[0]] = encoded
	return nil
}

func deleteRaw(root map[string]json.RawMessage, path ConfigPath) error {
	if len(path) == 1 {
		delete(root, path[0])
		return nil
	}
	raw, exists := root[path[0]]
	if !exists {
		return nil
	}
	var child map[string]json.RawMessage
	if err := json.Unmarshal(raw, &child); err != nil || child == nil {
		return fmt.Errorf("%w: parent %s is not an object", ErrConfigPath, formatPath(path[:1]))
	}
	if err := deleteRaw(child, path[1:]); err != nil {
		return err
	}
	encoded, err := json.Marshal(child)
	if err != nil {
		return fmt.Errorf("encode config path %s: %w", formatPath(path[:1]), err)
	}
	root[path[0]] = encoded
	return nil
}

func sameJSONValue(left json.RawMessage, leftExists bool, right json.RawMessage, rightExists bool) bool {
	if leftExists != rightExists {
		return false
	}
	if !leftExists {
		return true
	}
	var leftValue any
	var rightValue any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&leftValue) != nil || rightDecoder.Decode(&rightValue) != nil {
		return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func managedOwner(root map[string]json.RawMessage, path ConfigPath) (string, bool, error) {
	raw, exists := root[managedMetadataKey]
	if !exists {
		return "", false, nil
	}
	var owners map[string]string
	if err := json.Unmarshal(raw, &owners); err != nil || owners == nil {
		return "", false, fmt.Errorf("decode %s metadata: %w", managedMetadataKey, err)
	}
	owner, exists := owners[managedPathKey(path)]
	return owner, exists, nil
}

func setManagedOwner(root map[string]json.RawMessage, path ConfigPath, owner string) error {
	owners, err := managedOwners(root)
	if err != nil {
		return err
	}
	owners[managedPathKey(path)] = owner
	encoded, err := json.Marshal(owners)
	if err != nil {
		return fmt.Errorf("encode %s metadata: %w", managedMetadataKey, err)
	}
	root[managedMetadataKey] = encoded
	return nil
}

func deleteManagedOwner(root map[string]json.RawMessage, path ConfigPath) error {
	owners, err := managedOwners(root)
	if err != nil {
		return err
	}
	delete(owners, managedPathKey(path))
	if len(owners) == 0 {
		delete(root, managedMetadataKey)
		return nil
	}
	encoded, err := json.Marshal(owners)
	if err != nil {
		return fmt.Errorf("encode %s metadata: %w", managedMetadataKey, err)
	}
	root[managedMetadataKey] = encoded
	return nil
}

func managedOwners(root map[string]json.RawMessage) (map[string]string, error) {
	owners := make(map[string]string)
	raw, exists := root[managedMetadataKey]
	if !exists {
		return owners, nil
	}
	if err := json.Unmarshal(raw, &owners); err != nil || owners == nil {
		return nil, fmt.Errorf("decode %s metadata: %w", managedMetadataKey, err)
	}
	return owners, nil
}

func managedPathKey(path ConfigPath) string {
	parts := make([]string, len(path))
	for i, segment := range path {
		segment = strings.ReplaceAll(segment, "~", "~0")
		segment = strings.ReplaceAll(segment, "/", "~1")
		parts[i] = segment
	}
	return "/" + strings.Join(parts, "/")
}

func formatPath(path ConfigPath) string {
	if len(path) == 0 {
		return "/"
	}
	return managedPathKey(path)
}
