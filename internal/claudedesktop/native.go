package claudedesktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

var (
	// ErrUnsupportedHost reports a host where Benes may not manage Claude
	// Desktop native configuration at all.
	ErrUnsupportedHost = errors.New("claude desktop native configuration is not supported on this host")
	// ErrNativeUnreadable reports a configuration file that could not be read.
	ErrNativeUnreadable = errors.New("claude desktop configuration could not be read")
	// ErrNativeNotRegular reports a configuration path that is not a regular
	// file, such as a directory or a symlink.
	ErrNativeNotRegular = errors.New("claude desktop configuration is not a regular file")
	// ErrNativeMalformed reports a configuration file that is not a JSON
	// object. Benes fails closed instead of overwriting what it cannot parse.
	ErrNativeMalformed = errors.New("claude desktop configuration is not a JSON object")
	// ErrNativeBlocked reports a non-object value where the managed entry would
	// have to live.
	ErrNativeBlocked = errors.New("claude desktop configuration holds a non-object mcpServers value")
	// ErrForeignEntry reports a mcpServers.benes value that is not a
	// well-formed Benes-owned projection.
	ErrForeignEntry = errors.New("mcpServers.benes is not a benes-owned projection")
	// ErrVerificationFailed reports a write that did not produce the requested
	// native projection when re-read.
	ErrVerificationFailed = errors.New("claude desktop configuration did not verify after write")
)

// ObservedKind names what the native document actually contains at the
// Benes-owned key path.
type ObservedKind string

const (
	// ObservedUnobserved means no observation was performed, because the host
	// is unsupported, the client is not installed, or the target is unreadable.
	ObservedUnobserved ObservedKind = "unobserved"
	// ObservedConfigAbsent means the configuration file does not exist.
	ObservedConfigAbsent ObservedKind = "config_absent"
	// ObservedConfigUnparsable means the configuration file is not a JSON
	// object.
	ObservedConfigUnparsable ObservedKind = "config_unparsable"
	// ObservedNoMCPServers means the document has no mcpServers key.
	ObservedNoMCPServers ObservedKind = "no_mcp_servers"
	// ObservedMCPServersUnusable means mcpServers is present but is not an
	// object, so no entry can be read or written without replacing user data.
	ObservedMCPServersUnusable ObservedKind = "mcp_servers_unusable"
	// ObservedNoBenesEntry means mcpServers is usable and has no benes entry.
	ObservedNoBenesEntry ObservedKind = "no_benes_entry"
	// ObservedEntryUnusable means a benes entry exists but is not a well-formed
	// managed projection.
	ObservedEntryUnusable ObservedKind = "benes_entry_unusable"
	// ObservedEntryPresent means a well-formed managed projection is present.
	ObservedEntryPresent ObservedKind = "benes_entry_present"
)

// Observed is the native document's Benes-owned state, derived only from the
// file on disk.
type Observed struct {
	Kind  ObservedKind
	Entry *Projection
}

// Native is a parsed Claude Desktop configuration document.
//
// Every key outside the Benes-owned entry is retained as raw JSON, so unrelated
// user configuration survives a rewrite without being reinterpreted or
// re-encoded.
type Native struct {
	root   map[string]json.RawMessage
	exists bool
}

// Exists reports whether the configuration file was present.
func (n *Native) Exists() bool { return n != nil && n.exists }

// ReadNative reads and parses the configuration at path. A missing file is not
// an error: it yields an empty document, because the managed entry may create a
// document that does not exist yet. Every other failure is returned so callers
// fail closed.
func ReadNative(path string) (*Native, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Native{root: map[string]json.RawMessage{}}, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrNativeUnreadable, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNativeNotRegular, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNativeUnreadable, err)
	}
	native, err := parseNative(data)
	if err != nil {
		return nil, err
	}
	native.exists = true
	return native, nil
}

// parseNative decodes a configuration document, or fails closed.
func parseNative(data []byte) (*Native, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, ErrNativeMalformed
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return nil, ErrNativeMalformed
	}
	// json.Unmarshal accepts a literal null into a map and leaves it nil; a
	// document that is not an object is not a configuration Benes may rewrite.
	if root == nil {
		return nil, ErrNativeMalformed
	}
	return &Native{root: root}, nil
}

// Observe reports the Benes-owned state of the document.
func (n *Native) Observe() Observed {
	if n == nil || n.root == nil {
		return Observed{Kind: ObservedUnobserved}
	}
	raw, ok := n.root[mcpServersKey]
	if !ok {
		return Observed{Kind: ObservedNoMCPServers}
	}
	servers, ok := decodeObject(raw)
	if !ok {
		return Observed{Kind: ObservedMCPServersUnusable}
	}
	entry, ok := servers[ManagedEntryName]
	if !ok {
		return Observed{Kind: ObservedNoBenesEntry}
	}
	projection, ok := decodeProjection(entry)
	if !ok {
		return Observed{Kind: ObservedEntryUnusable}
	}
	return Observed{Kind: ObservedEntryPresent, Entry: &projection}
}

// cloneRoot copies the document so a mutation never edits the parsed original.
func (n *Native) cloneRoot() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(n.root)+1)
	for key, value := range n.root {
		out[key] = value
	}
	return out
}

// decodeObject decodes raw as a JSON object. A literal null, an array, or a
// scalar is not an object.
func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

// decodeProjection decodes a managed mcpServers entry.
//
// The entry must be an object whose keys are exactly command and, optionally,
// args. Any additional key means the entry is not a Benes-owned projection, so
// Benes reports it as unusable rather than adopting or overwriting it.
func decodeProjection(raw json.RawMessage) (Projection, bool) {
	fields, ok := decodeObject(raw)
	if !ok {
		return Projection{}, false
	}
	for key := range fields {
		if key != "command" && key != "args" {
			return Projection{}, false
		}
	}
	if _, ok := fields["command"]; !ok {
		return Projection{}, false
	}
	var projection Projection
	if err := json.Unmarshal(fields["command"], &projection.Command); err != nil {
		return Projection{}, false
	}
	if rawArgs, ok := fields["args"]; ok {
		if err := json.Unmarshal(rawArgs, &projection.Args); err != nil {
			return Projection{}, false
		}
	}
	if !projection.Valid() {
		return Projection{}, false
	}
	return projection, true
}

// encodeObject serializes a JSON object with sorted keys. Values are emitted
// from their raw encoding, so every entry Benes does not own keeps its exact
// original bytes.
//
// It returns an error rather than an empty value, so a caller can never publish
// a document whose managed container silently disappeared.
func encodeObject(entries map[string]json.RawMessage) (json.RawMessage, error) {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("encode mcpServers key: %w", err)
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(bytes.TrimSpace(entries[key]))
	}
	buf.WriteByte('}')
	return json.RawMessage(buf.Bytes()), nil
}

// encodeDocument serializes a configuration document with sorted root keys and
// one key per line. Unrelated values are written from their raw encoding, so
// user-owned configuration is preserved as it was read.
func encodeDocument(root map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range keys {
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.WriteString("  ")
		buf.Write(name)
		buf.WriteString(": ")
		buf.Write(bytes.TrimSpace(root[key]))
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
