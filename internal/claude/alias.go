package claude

import (
	"strings"
)

const (
	aliasPrefixV1        = "claude-benes-"
	aliasPrefixV2        = "claude-benes2-"
	nativePseudoProvider = "native"
	aliasSlashEnc        = "~s"
	aliasTildeEnc        = "~t"
)

func modelNeedsEscape(modelID string) bool {
	return strings.Contains(modelID, "/") || strings.Contains(modelID, "~")
}

func encodeModelID(modelID string) string {
	return strings.ReplaceAll(strings.ReplaceAll(modelID, "~", aliasTildeEnc), "/", aliasSlashEnc)
}

func AliasForRoute(provider, modelID string) string {
	if provider == "" || strings.Contains(provider, "--") || strings.Contains(provider, "/") || provider == nativePseudoProvider {
		return ""
	}
	if modelID == "" {
		return ""
	}
	if provider == "anthropic" && strings.HasPrefix(modelID, "claude-") {
		return modelID
	}
	if modelNeedsEscape(modelID) {
		return aliasPrefixV2 + provider + "--" + encodeModelID(modelID)
	}
	return aliasPrefixV1 + provider + "--" + modelID
}

func AliasForNative(slug string) string {
	if slug == "" || strings.Contains(slug, "/") || strings.Contains(slug, "--") {
		return ""
	}
	if modelNeedsEscape(slug) {
		return aliasPrefixV2 + nativePseudoProvider + "--" + encodeModelID(slug)
	}
	return aliasPrefixV1 + nativePseudoProvider + "--" + slug
}

func SurfaceAlias(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	slash := strings.Index(entry, "/")
	if slash > 0 {
		provider := entry[:slash]
		id := entry[slash+1:]
		if alias := AliasForRoute(provider, id); alias != "" {
			return alias
		}
		return entry
	}
	if alias := AliasForNative(entry); alias != "" {
		return alias
	}
	return entry
}
