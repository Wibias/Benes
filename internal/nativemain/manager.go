package nativemain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type Manager struct {
	Context Context
	Keys    KeyProvider
	Now     func() time.Time
	UUID    func() string
	Probe   func() error
	mu      sync.Mutex
}

func NewManager(codexHome, configDir string, keys KeyProvider) (*Manager, error) {
	ctx, err := ResolveContext(codexHome, configDir)
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = NewOSKeyProvider()
	}
	return &Manager{
		Context: ctx,
		Keys:    keys,
		Now:     time.Now,
		UUID:    newUUID,
		Probe:   func() error { return nil },
	}, nil
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexs := hex.EncodeToString(b[:])
	return hexs[0:8] + "-" + hexs[8:12] + "-" + hexs[12:16] + "-" + hexs[16:20] + "-" + hexs[20:]
}

func (m *Manager) withLock(fn func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.Context.RootDir, 0o700); err != nil {
		return fail("PROFILE_STORAGE_UNSAFE", "The native-profile metadata root is not a private canonical directory.", 409)
	}
	return fn()
}

func validateLabel(label string) (string, error) {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" || utf8len(trimmed) > 64 {
		return "", fail("INVALID_REQUEST", "Profile labels must contain 1-64 printable characters.", 400)
	}
	for _, r := range trimmed {
		if r < 32 || r == 127 || unicode.IsControl(r) {
			return "", fail("INVALID_REQUEST", "Profile labels must contain 1-64 printable characters.", 400)
		}
	}
	return trimmed, nil
}

func utf8len(s string) int { return len([]rune(s)) }

func publicOf(rec Record) PublicProfile {
	return PublicProfile{ID: rec.ID, Label: rec.Label, IdentityHint: rec.IdentityHint, State: rec.State}
}

func (m *Manager) readVault() (*Vault, error) {
	raw, err := os.ReadFile(m.Context.VaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fail("VAULT_INVALID", "The encrypted native-profile vault is unreadable.", 409)
	}
	var vault Vault
	if json.Unmarshal(raw, &vault) != nil || vault.Version != 1 || vault.HomeID != m.Context.HomeID {
		return nil, fail("VAULT_INVALID", "The encrypted native-profile vault is invalid.", 409)
	}
	return &vault, nil
}

func (m *Manager) writeVault(vault *Vault) error {
	raw, err := json.Marshal(vault)
	if err != nil {
		return fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
	}
	return atomicfile.Write(m.Context.VaultPath, raw, atomicfile.Options{Mode: 0o600})
}

func (m *Manager) requireVault() (*Vault, error) {
	vault, err := m.readVault()
	if err != nil {
		return nil, err
	}
	if vault == nil {
		return nil, fail("PROFILE_NOT_FOUND", "No native main login profiles are registered.", 404)
	}
	return vault, nil
}

func (m *Manager) current(vault *Vault) (Record, error) {
	if vault.ActiveProfileID == nil {
		return Record{}, fail("PROFILE_NOT_FOUND", "No active native profile is registered.", 404)
	}
	for _, rec := range vault.Profiles {
		if rec.ID == *vault.ActiveProfileID {
			return rec, nil
		}
	}
	return Record{}, fail("PROFILE_NOT_FOUND", "The active native profile is missing.", 404)
}

func (m *Manager) keyForVault(vault *Vault) (*Key, error) {
	if vault == nil {
		return m.Keys.Create(m.Context.HomeID)
	}
	key, err := m.Keys.Get(m.Context.HomeID)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fail("KEYRING_KEY_MISSING", "The OS credential-store key for this CODEX_HOME is missing.", 409)
	}
	return key, nil
}

func (m *Manager) List() (ListResult, error) {
	vault, err := m.readVault()
	if err != nil {
		return ListResult{}, err
	}
	out := ListResult{EffectiveCodexHome: m.Context.CodexHome, Profiles: []PublicProfile{}}
	if vault == nil {
		return out, nil
	}
	out.ActiveProfileID = vault.ActiveProfileID
	for _, rec := range vault.Profiles {
		out.Profiles = append(out.Profiles, publicOf(rec))
	}
	return out, nil
}

func (m *Manager) recoveryState() string {
	if _, err := os.Lstat(m.Context.RecoveryBlockPath); err == nil {
		return "manual"
	}
	if _, err := os.Lstat(m.Context.JournalPath); err == nil {
		return "journal"
	}
	return "none"
}

func (m *Manager) Doctor() (map[string]any, error) {
	var out map[string]any
	err := m.withLock(func() error {
		mode := credentialStoreMode(m.Context.CodexHome)
		vault, vaultErr := m.readVault()
		vaultStatus := "missing"
		if vaultErr != nil {
			vaultStatus = "invalid"
		} else if vault != nil {
			vaultStatus = "ok"
		}
		keyStore := "available"
		key, err := m.Keys.Get(m.Context.HomeID)
		if err != nil {
			keyStore = "unavailable"
		} else if vaultStatus != "missing" && key == nil {
			keyStore = "missing-key"
		}
		var active any
		if vault != nil && vault.ActiveProfileID != nil {
			active = *vault.ActiveProfileID
		}
		var profileCount any
		if vaultStatus == "invalid" {
			profileCount = nil
		} else if vault != nil {
			profileCount = len(vault.Profiles)
		} else {
			profileCount = 0
		}
		out = map[string]any{
			"effectiveCodexHome":  m.Context.CodexHome,
			"credentialStoreMode": mode,
			"supported":           mode == "file",
			"authStatus":          peekAuth(m.Context.AuthPath),
			"keyStore":            keyStore,
			"vaultStatus":         vaultStatus,
			"profileCount":        profileCount,
			"activeProfileId":     active,
			"recoveryPending":     m.recoveryState() != "none",
			"recoveryState":       m.recoveryState(),
			"stagingSweep":        "ok",
			"stagingCount":        m.stageCount(),
		}
		return nil
	})
	return out, err
}

func credentialStoreMode(codexHome string) string {
	raw, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		return "file"
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.Split(line, "#")[0])
		if strings.HasPrefix(line, "cli_auth_credentials_store") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				return "unknown"
			}
			return strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		}
	}
	return "file"
}

func requireFileStore(codexHome string) error {
	mode := credentialStoreMode(codexHome)
	if mode != "file" {
		return fail("UNSUPPORTED_AUTH_STORE", `Native profile switching supports Codex credential-store mode "file" only; current mode is "`+mode+`".`, 409)
	}
	return nil
}

func (m *Manager) Register(labelInput string) (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		if m.recoveryState() != "none" {
			return fail("RECOVERY_REQUIRED", "A native-profile recovery journal is pending.", 409)
		}
		if err := requireFileStore(m.Context.CodexHome); err != nil {
			return err
		}
		label, err := validateLabel(labelInput)
		if err != nil {
			return err
		}
		env, err := readAuth(m.Context.AuthPath)
		if err != nil {
			return err
		}
		vault, err := m.readVault()
		if err != nil {
			return err
		}
		key, err := m.keyForVault(vault)
		if err != nil {
			return err
		}
		hash := identityHash(key.Raw, env.AccountID)
		now := m.Now().UTC().Format(time.RFC3339Nano)
		if vault == nil {
			id := m.UUID()
			vault = &Vault{Version: 1, Revision: 1, HomeID: m.Context.HomeID, ActiveProfileID: &id, Profiles: []Record{{
				ID: id, Label: label, IdentityHash: hash, IdentityHint: identityHint(hash), State: "active", CreatedAt: now, UpdatedAt: now,
			}}}
		} else {
			active, err := m.current(vault)
			if err != nil {
				return err
			}
			if identityHash(key.Raw, env.AccountID) != active.IdentityHash {
				return fail("ACTIVE_PROFILE_MISMATCH", "The physical native login changed outside Benes; recover or register the expected login before continuing.", 409)
			}
			for i := range vault.Profiles {
				if vault.Profiles[i].ID == active.ID {
					vault.Profiles[i].Label = label
					vault.Profiles[i].UpdatedAt = now
				}
			}
			vault.Revision++
		}
		if err := m.writeVault(vault); err != nil {
			return err
		}
		active, err := m.current(vault)
		if err != nil {
			return err
		}
		result = map[string]any{"effectiveCodexHome": m.Context.CodexHome, "profile": publicOf(active)}
		return nil
	})
	return result, err
}

func (m *Manager) readStages() StageRegistry {
	raw, err := os.ReadFile(m.Context.StageRegistryPath)
	if err != nil {
		return StageRegistry{Version: 1, HomeID: m.Context.HomeID}
	}
	var reg StageRegistry
	if json.Unmarshal(raw, &reg) != nil {
		return StageRegistry{Version: 1, HomeID: m.Context.HomeID}
	}
	return reg
}

func (m *Manager) writeStages(reg StageRegistry) error {
	raw, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	return atomicfile.Write(m.Context.StageRegistryPath, raw, atomicfile.Options{Mode: 0o600})
}

func (m *Manager) stageCount() int {
	return len(m.readStages().Stages)
}

func (m *Manager) PrepareStage() (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		if m.recoveryState() != "none" {
			return fail("RECOVERY_REQUIRED", "A native-profile recovery journal is pending.", 409)
		}
		if err := requireFileStore(m.Context.CodexHome); err != nil {
			return err
		}
		vault, err := m.requireVault()
		if err != nil {
			return err
		}
		key, err := m.keyForVault(vault)
		if err != nil {
			return err
		}
		env, err := readAuth(m.Context.AuthPath)
		if err != nil {
			return err
		}
		active, err := m.current(vault)
		if err != nil {
			return err
		}
		if identityHash(key.Raw, env.AccountID) != active.IdentityHash {
			return fail("ACTIVE_PROFILE_MISMATCH", "The physical native login changed outside Benes; recover or register the expected login before continuing.", 409)
		}
		if err := os.MkdirAll(m.Context.StagingRoot, 0o700); err != nil {
			return fail("PROFILE_STORAGE_UNSAFE", "The native-login staging root escaped BENES_HOME.", 409)
		}
		stageID, leaseID, token := m.UUID(), m.UUID(), m.UUID()+m.UUID()
		home := filepath.Join(m.Context.StagingRoot, stageID)
		if err := os.MkdirAll(home, 0o700); err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(home, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), atomicfile.Options{Mode: 0o600}); err != nil {
			return err
		}
		now := m.Now().UnixMilli()
		expires := now + stageLeaseMS
		reg := m.readStages()
		reg.Version = 1
		reg.HomeID = m.Context.HomeID
		reg.Revision++
		reg.Stages = append(reg.Stages, StageRecord{
			StageID: stageID, LeaseID: leaseID, WriterTokenHash: writerTokenHash(token),
			CreatorInstanceID: m.Context.InstanceID, StagingRoot: home, State: "open",
			CreatedAt: now, LastHeartbeatAt: now, LeaseExpiresAt: expires,
		})
		if err := m.writeStages(reg); err != nil {
			return err
		}
		result = map[string]any{
			"stageId":             stageID,
			"writerToken":         token,
			"stagingCodexHome":    home,
			"effectiveCodexHome":  m.Context.CodexHome,
			"leaseExpiresAt":      expires,
			"heartbeatIntervalMs": stageHeartbeatMS,
		}
		return nil
	})
	return result, err
}

func (m *Manager) findStage(stageID, token string) (StageRecord, int, error) {
	reg := m.readStages()
	for i, rec := range reg.Stages {
		if rec.StageID == stageID && rec.WriterTokenHash == writerTokenHash(token) {
			return rec, i, nil
		}
	}
	return StageRecord{}, -1, fail("STAGING_NOT_FOUND", "The native-login staging session was not found.", 404)
}

func (m *Manager) Heartbeat(stageID, token string) (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		rec, idx, err := m.findStage(stageID, token)
		if err != nil {
			return err
		}
		now := m.Now().UnixMilli()
		if now > rec.LeaseExpiresAt {
			return fail("STAGING_EXPIRED", "The native-login staging lease expired.", 410)
		}
		reg := m.readStages()
		reg.Stages[idx].LastHeartbeatAt = now
		reg.Stages[idx].LeaseExpiresAt = now + stageLeaseMS
		reg.Revision++
		if err := m.writeStages(reg); err != nil {
			return err
		}
		result = map[string]any{"ok": true, "leaseExpiresAt": reg.Stages[idx].LeaseExpiresAt}
		return nil
	})
	return result, err
}

func (m *Manager) Finish(stageID, token, labelInput string) (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		if m.recoveryState() != "none" {
			return fail("RECOVERY_REQUIRED", "A native-profile recovery journal is pending.", 409)
		}
		if err := requireFileStore(m.Context.CodexHome); err != nil {
			return err
		}
		rec, _, err := m.findStage(stageID, token)
		if err != nil {
			return err
		}
		if m.Now().UnixMilli() > rec.LeaseExpiresAt {
			return fail("STAGING_EXPIRED", "The native-login staging lease expired.", 410)
		}
		label, err := validateLabel(labelInput)
		if err != nil {
			return err
		}
		target, err := readAuth(filepath.Join(rec.StagingRoot, "auth.json"))
		if err != nil {
			return err
		}
		current, err := readAuth(m.Context.AuthPath)
		if err != nil {
			return err
		}
		vault, err := m.requireVault()
		if err != nil {
			return err
		}
		if len(vault.Profiles) >= maxNativeProfiles {
			return fail("INVALID_REQUEST", "Native profiles are limited to 32 entries.", 400)
		}
		key, err := m.keyForVault(vault)
		if err != nil {
			return err
		}
		active, err := m.current(vault)
		if err != nil {
			return err
		}
		if identityHash(key.Raw, current.AccountID) != active.IdentityHash {
			return fail("ACTIVE_PROFILE_MISMATCH", "The physical native login changed outside Benes; recover or register the expected login before continuing.", 409)
		}
		id := m.UUID()
		hash := identityHash(key.Raw, target.AccountID)
		for _, rec := range vault.Profiles {
			if rec.IdentityHash == hash || strings.EqualFold(rec.Label, label) {
				return fail("PROFILE_ALREADY_EXISTS", "That native identity is already registered.", 409)
			}
		}
		now := m.Now().UTC().Format(time.RFC3339Nano)
		payload, err := encryptEnvelope(m.Context, id, hash, target, key)
		if err != nil {
			return err
		}
		profile := Record{ID: id, Label: label, IdentityHash: hash, IdentityHint: identityHint(hash), State: "inactive", Payload: &payload, CreatedAt: now, UpdatedAt: now}
		vault.Profiles = append(vault.Profiles, profile)
		vault.Revision++
		if err := m.writeVault(vault); err != nil {
			return err
		}
		_, _, _ = m.removeStage(stageID, token, false)
		result = map[string]any{"effectiveCodexHome": m.Context.CodexHome, "profile": publicOf(profile), "plaintextMayRemain": false}
		return nil
	})
	return result, err
}

func (m *Manager) Cancel(stageID, token string) (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		removed, remain, err := m.removeStage(stageID, token, true)
		if err != nil {
			return err
		}
		result = map[string]any{"ok": true, "removed": removed, "plaintextMayRemain": remain}
		return nil
	})
	return result, err
}

func (m *Manager) removeStage(stageID, token string, requireToken bool) (bool, bool, error) {
	reg := m.readStages()
	kept := reg.Stages[:0]
	var rec *StageRecord
	for _, item := range reg.Stages {
		if item.StageID == stageID && (!requireToken || item.WriterTokenHash == writerTokenHash(token)) {
			copy := item
			rec = &copy
			continue
		}
		kept = append(kept, item)
	}
	if rec == nil {
		return false, false, fail("STAGING_NOT_FOUND", "The native-login staging session was not found.", 404)
	}
	reg.Stages = kept
	reg.Revision++
	_ = os.RemoveAll(rec.StagingRoot)
	if err := m.writeStages(reg); err != nil {
		return false, true, err
	}
	return true, false, nil
}

func (m *Manager) resolveTarget(vault *Vault, selector string) (Record, error) {
	key := strings.ToLower(strings.TrimSpace(selector))
	var found []Record
	for _, rec := range vault.Profiles {
		if strings.ToLower(rec.ID) == key || strings.ToLower(rec.Label) == key {
			found = append(found, rec)
		}
	}
	if len(found) != 1 {
		return Record{}, fail("PROFILE_NOT_FOUND", "The requested native profile was not found.", 404)
	}
	return found[0], nil
}

func (m *Manager) Switch(targetSelector string, confirmedStopped bool) (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		if !confirmedStopped {
			return fail("CODEX_BUSY", "Close Codex App/CLI, then pass --yes to confirm it is stopped.", 409)
		}
		if err := m.Probe(); err != nil {
			return err
		}
		if err := requireFileStore(m.Context.CodexHome); err != nil {
			return err
		}
		if m.recoveryState() == "journal" {
			if _, err := m.recoverLocked(false); err != nil {
				return err
			}
		} else if m.recoveryState() != "none" {
			return fail("RECOVERY_REQUIRED", "A native-profile recovery journal is pending.", 409)
		}
		before, err := m.requireVault()
		if err != nil {
			return err
		}
		key, err := m.keyForVault(before)
		if err != nil {
			return err
		}
		sourceEnv, err := readAuth(m.Context.AuthPath)
		if err != nil {
			return err
		}
		source, err := m.current(before)
		if err != nil {
			return err
		}
		if identityHash(key.Raw, sourceEnv.AccountID) != source.IdentityHash {
			return fail("ACTIVE_PROFILE_MISMATCH", "The physical native login changed outside Benes; recover or register the expected login before continuing.", 409)
		}
		target, err := m.resolveTarget(before, targetSelector)
		if err != nil {
			return err
		}
		if target.Payload == nil {
			return fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
		}
		targetEnv, err := decryptEnvelope(m.Context, target.ID, target.IdentityHash, *target.Payload, key)
		if err != nil {
			return err
		}
		now := m.Now().UTC().Format(time.RFC3339Nano)
		sourcePayload, err := encryptEnvelope(m.Context, source.ID, source.IdentityHash, sourceEnv, key)
		if err != nil {
			return err
		}
		after := *before
		after.Profiles = append([]Record{}, before.Profiles...)
		for i := range after.Profiles {
			if after.Profiles[i].ID == source.ID {
				after.Profiles[i].State = "inactive"
				p := sourcePayload
				after.Profiles[i].Payload = &p
				after.Profiles[i].UpdatedAt = now
			}
			if after.Profiles[i].ID == target.ID {
				after.Profiles[i].State = "active"
				after.Profiles[i].Payload = nil
				after.Profiles[i].UpdatedAt = now
			}
		}
		id := target.ID
		after.ActiveProfileID = &id
		after.Revision++
		if err := atomicfile.Write(m.Context.AuthPath, []byte(targetEnv.Text), atomicfile.Options{Mode: 0o600}); err != nil {
			return err
		}
		if err := m.writeVault(&after); err != nil {
			return err
		}
		active, _ := m.current(&after)
		result = map[string]any{"effectiveCodexHome": m.Context.CodexHome, "activeProfile": publicOf(active)}
		return nil
	})
	return result, err
}

func (m *Manager) Recover(rollback, confirmedStopped bool) (map[string]any, error) {
	var result map[string]any
	err := m.withLock(func() error {
		out, err := m.recoverLocked(rollback && confirmedStopped)
		result = out
		return err
	})
	return result, err
}

func (m *Manager) recoverLocked(rollback bool) (map[string]any, error) {
	if _, err := os.Stat(m.Context.JournalPath); os.IsNotExist(err) {
		return map[string]any{"effectiveCodexHome": m.Context.CodexHome, "recovered": false}, nil
	}
	raw, err := os.ReadFile(m.Context.JournalPath)
	if err != nil {
		return nil, fail("RECOVERY_REQUIRED", "The native-profile recovery journal is unreadable.", 409)
	}
	var journal Journal
	if json.Unmarshal(raw, &journal) != nil {
		return nil, fail("RECOVERY_REQUIRED", "The native-profile recovery journal is invalid.", 409)
	}
	_ = rollback
	if journal.Phase == "prepared" {
		_ = os.Remove(m.Context.JournalPath)
		return map[string]any{"effectiveCodexHome": m.Context.CodexHome, "recovered": true, "action": "rollback-source"}, nil
	}
	if journal.Phase == "vault-committed" || journal.Phase == "auth-replaced" {
		_ = m.writeVault(&journal.AfterVault)
		_ = os.Remove(m.Context.JournalPath)
		return map[string]any{"effectiveCodexHome": m.Context.CodexHome, "recovered": true, "action": "commit-target"}, nil
	}
	return map[string]any{"effectiveCodexHome": m.Context.CodexHome, "recovered": true, "action": "converged"}, nil
}
