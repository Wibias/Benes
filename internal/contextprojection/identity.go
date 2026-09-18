package contextprojection

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

const RefDomain = "benes-context-ref-v1"

func ValidIdentityInput(toolCallID, toolName string) bool {
	return strings.TrimSpace(toolCallID) != "" && strings.TrimSpace(toolName) != ""
}

func IdentityKey(toolCallID, toolNamespace, toolName string) string {
	return strings.Join([]string{toolCallID, toolNamespace, toolName}, "\x00")
}

func ArtifactRef(identity Identity) string {
	canonical := strings.Join([]string{
		RefDomain,
		identity.ToolCallID,
		identity.ToolNamespace,
		identity.ToolName,
		strconv.Itoa(identity.OccurrenceOrdinal),
	}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	digest := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(digest) > 24 {
		digest = digest[:24]
	}
	return "ctx_" + digest
}
