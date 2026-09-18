package claudedesktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ErrInvalidProjection reports a projection Benes refuses to serialize.
var ErrInvalidProjection = errors.New("benes-managed claude desktop projection is invalid")

// Projection is the exact Benes-owned value of the mcpServers.benes entry.
//
// The projection carries only what a documented MCP server definition carries
// for this entry: an executable and its argument list. It deliberately has no
// environment map, because the supported contract would require a secret value
// there and Benes does not persist secrets into a native projection.
type Projection struct {
	Command string
	Args    []string
}

// Valid reports whether p can be serialized as a managed projection.
func (p Projection) Valid() bool {
	if strings.TrimSpace(p.Command) == "" {
		return false
	}
	for _, arg := range p.Args {
		if strings.ContainsRune(arg, 0) {
			return false
		}
	}
	return true
}

// canonicalFields is the deterministic, sorted, formatting-independent view of
// the managed projection. Equivalent projections always produce identical
// fields; changing either the command or any argument changes them.
func (p Projection) canonicalFields() map[string]any {
	args := make([]string, 0, len(p.Args))
	args = append(args, p.Args...)
	return map[string]any{
		"args":    args,
		"command": p.Command,
	}
}

// Canonical returns the canonical JSON encoding of the managed projection.
// Object keys are sorted by encoding/json, so JSON formatting and key order in
// the native document cannot influence the result.
func (p Projection) Canonical() []byte {
	body, err := json.Marshal(p.canonicalFields())
	if err != nil {
		return nil
	}
	return body
}

// Fingerprint is the stable identity of the managed projection. It is sha256
// over Canonical, so it is deterministic for equivalent projections, changes
// when a managed semantic field changes, ignores irrelevant formatting and key
// order, and never encodes a secret: no secret may enter Projection.
func (p Projection) Fingerprint() string {
	canonical := p.Canonical()
	if canonical == nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// Equal reports semantic equality. Argument order is significant because the
// argument list is the executable's own contract; a reordered argument list is
// a different projection.
func (p Projection) Equal(other Projection) bool {
	if p.Command != other.Command {
		return false
	}
	if len(p.Args) != len(other.Args) {
		return false
	}
	for i := range p.Args {
		if p.Args[i] != other.Args[i] {
			return false
		}
	}
	return true
}

// DesiredProjection returns the projection Benes should have installed for the
// desired state, or nil when there is nothing to manage.
func DesiredProjection(in Input) *Projection {
	if !in.DesiredEnabled {
		return nil
	}
	return in.Managed
}

// ManagedNativeProjection reports the native projection Benes would bind into
// mcpServers.benes for Claude Desktop.
//
// Benes ships no stdio MCP runtime, so there is no executable projection to
// bind and this reports nil. Writing a placeholder, empty, or dangling entry
// merely to make an apply mutate a file would misrepresent a Benes-managed
// native integration that does not exist; the runtime contract reports the
// absence instead. A future stdio MCP runtime would make this return a
// projection, and only then would apply become executable.
func ManagedNativeProjection() *Projection {
	return nil
}

// RuntimeUnavailableMessage is the operator-facing text for a refused apply. It
// is never machine-decoded, so it stays a message and not a code.
const RuntimeUnavailableMessage = "Benes ships no Claude Desktop MCP runtime, so there is no mcpServers.benes entry it may install. Benes refuses to write a placeholder or dangling native entry."
