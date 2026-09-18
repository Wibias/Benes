package config

import (
	"encoding/json"
	"strings"
)

// OpenAIDefaultAccess returns the authoritative logical OpenAI access method.
// Invalid, absent, or unreadable values fail closed to the historical OAuth
// default rather than inventing an API selection.
func OpenAIDefaultAccess(disk DiskConfig) string {
	raw, ok := disk.Providers[LogicalOpenAIID]
	if !ok {
		return DefaultAccessOAuth
	}
	var record struct {
		DefaultAccess string `json:"defaultAccess"`
	}
	if json.Unmarshal(raw, &record) != nil {
		return DefaultAccessOAuth
	}
	if strings.TrimSpace(record.DefaultAccess) == DefaultAccessAPI {
		return DefaultAccessAPI
	}
	return DefaultAccessOAuth
}

// OpenAIDefaultConnection returns the physical connection selected by the
// logical OpenAI defaultAccess setting. It intentionally does not inspect
// catalog or credential availability: the configured lane is authoritative,
// and an unavailable selected lane must fail rather than silently crossing to
// the other credential contract.
func OpenAIDefaultConnection(defaultAccess string) string {
	if strings.TrimSpace(defaultAccess) == DefaultAccessAPI {
		return OpenAIAPIConnection
	}
	return LogicalOpenAIID
}
