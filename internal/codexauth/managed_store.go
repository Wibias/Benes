package codexauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const maxManagedStoreBytes = 16 << 20

var (
	ErrManagedCredentialGenerationConflict = errors.New("managed Codex credential changed during update")
	ErrManagedCredentialStoreInvalid       = errors.New("managed Codex credential store is invalid")
	ErrManagedCredentialStoreUnreadable    = errors.New("managed Codex credential store is unreadable")
)

type ManagedCredentialStoreStatus string

const (
	ManagedCredentialStoreOK         ManagedCredentialStoreStatus = "ok"
	ManagedCredentialStoreMissing    ManagedCredentialStoreStatus = "missing"
	ManagedCredentialStoreInvalid    ManagedCredentialStoreStatus = "invalid"
	ManagedCredentialStoreUnreadable ManagedCredentialStoreStatus = "unreadable"
)

type ManagedCredential struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresAtMS      int64  `json:"expiresAt"`
	ChatGPTAccountID string `json:"chatgptAccountId"`
}

type ManagedCredentialRecord struct {
	Credential              *ManagedCredential `json:"credential,omitempty"`
	Generation              int64              `json:"generation"`
	RefreshGrantFingerprint string             `json:"refreshGrantFingerprint,omitempty"`
	DeletedAtMS             *int64             `json:"deletedAt,omitempty"`
	ReplacedAtMS            *int64             `json:"replacedAt,omitempty"`
	LastValidatedAtMS       *int64             `json:"lastCodexValidatedAt,omitempty"`
	LastValidationStatus    string             `json:"lastCodexValidationStatus,omitempty"`
	LastValidationError     string             `json:"lastCodexValidationError,omitempty"`
}

type ManagedCredentialSnapshot struct {
	Status  ManagedCredentialStoreStatus
	Records map[string]ManagedCredentialRecord
}

type ManagedCredentialStore struct {
	path     string
	lockPath string
	openFile func(string) (*os.File, error)
}

func NewManagedCredentialStore(benesHome string) (*ManagedCredentialStore, error) {
	if strings.TrimSpace(benesHome) == "" || !filepath.IsAbs(benesHome) {
		return nil, fmt.Errorf("absolute Benes home is required")
	}
	path := filepath.Join(filepath.Clean(benesHome), "codex-accounts.json")
	return &ManagedCredentialStore{
		path:     path,
		lockPath: path + ".lock",
		openFile: os.Open,
	}, nil
}

func (s *ManagedCredentialStore) Read() ManagedCredentialSnapshot {
	file, err := s.openFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return managedSnapshot(ManagedCredentialStoreMissing, nil)
		}
		return managedSnapshot(ManagedCredentialStoreUnreadable, nil)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return managedSnapshot(ManagedCredentialStoreUnreadable, nil)
	}
	if !info.Mode().IsRegular() || info.Size() > maxManagedStoreBytes {
		return managedSnapshot(ManagedCredentialStoreInvalid, nil)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return managedSnapshot(ManagedCredentialStoreInvalid, nil)
	}

	raw, err := io.ReadAll(io.LimitReader(file, maxManagedStoreBytes+1))
	if err != nil {
		return managedSnapshot(ManagedCredentialStoreUnreadable, nil)
	}
	if len(raw) > maxManagedStoreBytes {
		return managedSnapshot(ManagedCredentialStoreInvalid, nil)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return managedSnapshot(ManagedCredentialStoreInvalid, nil)
	}
	records := make(map[string]ManagedCredentialRecord, len(root))
	for id, value := range root {
		if record, ok := normalizeManagedCredentialRecord(value); ok {
			records[id] = record
		}
	}
	return managedSnapshot(ManagedCredentialStoreOK, records)
}

func (s *ManagedCredentialStore) CompareAndSwap(
	ctx context.Context,
	accountID string,
	generation int64,
	credential ManagedCredential,
) (int64, error) {
	lock, err := acquireManagedStoreMutationLock(ctx, s.lockPath)
	if err != nil {
		return 0, err
	}
	defer lock.Close()

	snapshot := s.Read()
	switch snapshot.Status {
	case ManagedCredentialStoreOK:
	case ManagedCredentialStoreInvalid:
		return 0, ErrManagedCredentialStoreInvalid
	case ManagedCredentialStoreUnreadable:
		return 0, ErrManagedCredentialStoreUnreadable
	case ManagedCredentialStoreMissing:
		return 0, ErrManagedCredentialGenerationConflict
	default:
		return 0, ErrManagedCredentialStoreInvalid
	}

	current, exists := snapshot.Records[accountID]
	if !exists || current.Generation != generation || current.DeletedAtMS != nil || current.Credential == nil {
		return 0, ErrManagedCredentialGenerationConflict
	}

	fingerprint := current.RefreshGrantFingerprint
	if current.Credential.RefreshToken != credential.RefreshToken || fingerprint == "" {
		fingerprint = RefreshGrantFingerprintForToken(credential.RefreshToken)
	}
	updated := credential
	nextGeneration := generation + 1
	snapshot.Records[accountID] = ManagedCredentialRecord{
		Credential:              &updated,
		Generation:              nextGeneration,
		RefreshGrantFingerprint: fingerprint,
		ReplacedAtMS:            cloneOptionalInt64(current.ReplacedAtMS),
		LastValidatedAtMS:       cloneOptionalInt64(current.LastValidatedAtMS),
		LastValidationStatus:    current.LastValidationStatus,
		LastValidationError:     current.LastValidationError,
	}
	if err := persistManagedCredentialRecords(s.path, snapshot.Records); err != nil {
		return 0, fmt.Errorf("persist managed Codex credential store: %w", err)
	}
	return nextGeneration, nil
}

func (s *ManagedCredentialStore) Put(ctx context.Context, accountID string, credential ManagedCredential, validatedAtMS *int64) error {
	return s.PutChecked(ctx, accountID, credential, validatedAtMS, nil)
}

func (s *ManagedCredentialStore) PutChecked(ctx context.Context, accountID string, credential ManagedCredential, validatedAtMS *int64, gate func() error) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("account id is required")
	}
	lock, err := acquireManagedStoreMutationLock(ctx, s.lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	snapshot := s.Read()
	switch snapshot.Status {
	case ManagedCredentialStoreOK, ManagedCredentialStoreMissing:
	case ManagedCredentialStoreInvalid:
		return ErrManagedCredentialStoreInvalid
	case ManagedCredentialStoreUnreadable:
		return ErrManagedCredentialStoreUnreadable
	default:
		return ErrManagedCredentialStoreInvalid
	}
	if snapshot.Records == nil {
		snapshot.Records = map[string]ManagedCredentialRecord{}
	}
	current := snapshot.Records[accountID]
	generation := current.Generation + 1
	if generation < 1 {
		generation = 1
	}
	updated := credential
	status := "ok"
	snapshot.Records[accountID] = ManagedCredentialRecord{
		Credential:              &updated,
		Generation:              generation,
		RefreshGrantFingerprint: RefreshGrantFingerprintForToken(credential.RefreshToken),
		LastValidatedAtMS:       cloneOptionalInt64(validatedAtMS),
		LastValidationStatus:    status,
	}
	if gate != nil {
		if err := gate(); err != nil {
			return err
		}
	}
	if err := persistManagedCredentialRecords(s.path, snapshot.Records); err != nil {
		return fmt.Errorf("persist managed Codex credential store: %w", err)
	}
	return nil
}

func RefreshGrantFingerprintForToken(refreshToken string) string {
	sum := sha256.Sum256([]byte("codex-refresh-grant:" + refreshToken))
	return hex.EncodeToString(sum[:])
}

func managedSnapshot(status ManagedCredentialStoreStatus, records map[string]ManagedCredentialRecord) ManagedCredentialSnapshot {
	if records == nil {
		records = make(map[string]ManagedCredentialRecord)
	}
	return ManagedCredentialSnapshot{Status: status, Records: records}
}

func normalizeManagedCredentialRecord(raw json.RawMessage) (ManagedCredentialRecord, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return ManagedCredentialRecord{}, false
	}
	if _, hasGeneration := object["generation"]; !hasGeneration {
		credential, ok := decodeManagedCredential(raw)
		if !ok {
			return ManagedCredentialRecord{}, false
		}
		return ManagedCredentialRecord{
			Credential:              &credential,
			Generation:              0,
			RefreshGrantFingerprint: RefreshGrantFingerprintForToken(credential.RefreshToken),
		}, true
	}

	generation, ok := requiredIntegerField(object, "generation")
	if !ok {
		return ManagedCredentialRecord{}, false
	}
	record := ManagedCredentialRecord{Generation: generation}
	if value, exists := object["credential"]; exists {
		credential, ok := decodeManagedCredential(value)
		if !ok {
			return ManagedCredentialRecord{}, false
		}
		record.Credential = &credential
	}
	if value, exists := object["refreshGrantFingerprint"]; exists {
		if json.Unmarshal(value, &record.RefreshGrantFingerprint) != nil {
			return ManagedCredentialRecord{}, false
		}
	}
	if record.RefreshGrantFingerprint == "" && record.Credential != nil {
		record.RefreshGrantFingerprint = RefreshGrantFingerprintForToken(record.Credential.RefreshToken)
	}
	if value, exists := object["deletedAt"]; exists {
		parsed, ok := integerJSON(value)
		if !ok {
			return ManagedCredentialRecord{}, false
		}
		record.DeletedAtMS = &parsed
	}
	if value, exists := object["replacedAt"]; exists {
		parsed, ok := integerJSON(value)
		if !ok {
			return ManagedCredentialRecord{}, false
		}
		record.ReplacedAtMS = &parsed
	}
	if value, exists := object["lastCodexValidatedAt"]; exists {
		parsed, ok := integerJSON(value)
		if !ok {
			return ManagedCredentialRecord{}, false
		}
		record.LastValidatedAtMS = &parsed
	}
	if value, exists := object["lastCodexValidationStatus"]; exists {
		if json.Unmarshal(value, &record.LastValidationStatus) != nil ||
			(record.LastValidationStatus != "ok" && record.LastValidationStatus != "failed") {
			return ManagedCredentialRecord{}, false
		}
	}
	if value, exists := object["lastCodexValidationError"]; exists {
		if json.Unmarshal(value, &record.LastValidationError) != nil {
			return ManagedCredentialRecord{}, false
		}
	}
	return record, true
}

func decodeManagedCredential(raw json.RawMessage) (ManagedCredential, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return ManagedCredential{}, false
	}
	var credential ManagedCredential
	for key, destination := range map[string]*string{
		"accessToken":      &credential.AccessToken,
		"refreshToken":     &credential.RefreshToken,
		"chatgptAccountId": &credential.ChatGPTAccountID,
	} {
		value, exists := object[key]
		if !exists || json.Unmarshal(value, destination) != nil {
			return ManagedCredential{}, false
		}
	}
	expiresAt, ok := requiredIntegerField(object, "expiresAt")
	if !ok {
		return ManagedCredential{}, false
	}
	credential.ExpiresAtMS = expiresAt
	return credential, true
}

func requiredIntegerField(object map[string]json.RawMessage, key string) (int64, bool) {
	value, exists := object[key]
	if !exists {
		return 0, false
	}
	return integerJSON(value)
}

func integerJSON(raw json.RawMessage) (int64, bool) {
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, false
	}
	value, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return value, true
}

func persistManagedCredentialRecords(path string, records map[string]ManagedCredentialRecord) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}

	body, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > maxManagedStoreBytes {
		return ErrManagedCredentialStoreInvalid
	}

	temp, err := os.CreateTemp(dir, ".codex-accounts-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(body); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFileAtomic(tempPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func cloneOptionalInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
