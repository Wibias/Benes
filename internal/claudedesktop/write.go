package claudedesktop

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

// MutationResult reports what a native mutation actually did.
//
// RestartRequired is set whenever the configuration file was rewritten, because
// Claude Desktop reads its configuration at startup and Benes has no evidence
// that a running instance reloaded the file.
type MutationResult struct {
	ClientID        string `json:"clientId"`
	Changed         bool   `json:"changed"`
	Applied         bool   `json:"applied"`
	ConfigPath      string `json:"configPath,omitempty"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	RestartRequired bool   `json:"restartRequired"`
}

// InstallNative merges p into the mcpServers.benes entry of the configuration
// at path, preserving every unrelated root key and every unrelated MCP server,
// then re-reads the file and verifies the projection it asked for.
//
// A document that cannot be parsed, a non-object mcpServers value, or a benes
// entry whose shape Benes does not own are all refused without writing.
func InstallNative(path string, p Projection) (MutationResult, error) {
	result := MutationResult{ClientID: ClientID, ConfigPath: path}
	if !p.Valid() {
		return result, ErrInvalidProjection
	}
	native, err := ReadNative(path)
	if err != nil {
		return result, err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := native.root[mcpServersKey]; ok {
		decoded, ok := decodeObject(raw)
		if !ok {
			return result, ErrNativeBlocked
		}
		servers = decoded
	}
	if existing, ok := servers[ManagedEntryName]; ok {
		observed, ok := decodeProjection(existing)
		if !ok {
			return result, ErrForeignEntry
		}
		if observed.Equal(p) {
			// An already-correct projection is not churned.
			result.Applied = true
			result.Fingerprint = p.Fingerprint()
			return result, nil
		}
		// Benes owns this entry name. A well-formed entry that differs is an
		// earlier Benes projection (or an edit of one), so it is replaced with
		// the desired projection rather than duplicated.
	}
	servers[ManagedEntryName] = json.RawMessage(p.Canonical())
	root := native.cloneRoot()
	encoded, err := encodeObject(servers)
	if err != nil {
		return result, err
	}
	root[mcpServersKey] = encoded
	if err := writeNative(path, root, native.Exists()); err != nil {
		return result, err
	}
	if err := verifyManagedEntry(path, &p); err != nil {
		return result, err
	}
	result.Changed = true
	result.Applied = true
	result.Fingerprint = p.Fingerprint()
	result.RestartRequired = true
	return result, nil
}

// RemoveNative removes the mcpServers.benes entry when, and only when, it is
// exactly p. Every other root key and MCP server is preserved.
//
// Absence is success: a document with no Benes-owned entry is already in the
// disabled state, so the result reports no change.
func RemoveNative(path string, p Projection) (MutationResult, error) {
	result := MutationResult{ClientID: ClientID, ConfigPath: path}
	if !p.Valid() {
		return result, ErrInvalidProjection
	}
	native, err := ReadNative(path)
	if err != nil {
		return result, err
	}
	if !native.Exists() {
		return result, nil
	}
	raw, ok := native.root[mcpServersKey]
	if !ok {
		return result, nil
	}
	servers, ok := decodeObject(raw)
	if !ok {
		return result, ErrNativeBlocked
	}
	entry, ok := servers[ManagedEntryName]
	if !ok {
		return result, nil
	}
	observed, ok := decodeProjection(entry)
	if !ok || !observed.Equal(p) {
		// Deleting an entry Benes cannot prove it wrote would destroy user
		// configuration, so the removal is refused instead.
		return result, ErrForeignEntry
	}
	delete(servers, ManagedEntryName)
	root := native.cloneRoot()
	if len(servers) == 0 {
		// An empty mcpServers object is semantically identical to no
		// mcpServers key for an MCP host, and Benes created this container.
		delete(root, mcpServersKey)
	} else {
		encoded, err := encodeObject(servers)
		if err != nil {
			return result, err
		}
		root[mcpServersKey] = encoded
	}
	if err := writeNative(path, root, native.Exists()); err != nil {
		return result, err
	}
	if err := verifyManagedEntry(path, nil); err != nil {
		return result, err
	}
	result.Changed = true
	result.RestartRequired = true
	return result, nil
}

// verifyManagedEntry re-reads the file and confirms the managed entry is what
// the caller asked for. want == nil means the entry must be gone.
//
// It returns ErrVerificationFailed rather than the underlying read error, so a
// caller can never mistake an unverifiable write for a successful one.
func verifyManagedEntry(path string, want *Projection) error {
	native, err := ReadNative(path)
	if err != nil {
		return ErrVerificationFailed
	}
	observed := native.Observe()
	if want == nil {
		if observed.Kind == ObservedNoBenesEntry || observed.Kind == ObservedNoMCPServers {
			return nil
		}
		return ErrVerificationFailed
	}
	if observed.Kind != ObservedEntryPresent || observed.Entry == nil {
		return ErrVerificationFailed
	}
	if !observed.Entry.Equal(*want) {
		return ErrVerificationFailed
	}
	return nil
}

// writeNative publishes the document atomically, creating the parent directory
// when needed and keeping the permissions of an existing file.
func writeNative(path string, root map[string]json.RawMessage, existed bool) error {
	body, err := encodeDocument(root)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if existed {
		if info, statErr := os.Stat(path); statErr == nil {
			if perm := info.Mode().Perm(); perm != 0 {
				mode = perm
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, body, atomicfile.Options{Mode: mode})
}
