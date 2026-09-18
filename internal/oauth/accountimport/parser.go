package accountimport

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	Provider             = "google-antigravity"
	Format               = "cockpit-tools"
	MaxBytes             = 256 * 1024
	MaxRequestBytes      = MaxBytes + 1024
	MaxRecords           = 25
	MaxEmailLength       = 254
	MaxRefreshTokenBytes = 16 * 1024
)

var whitespaceOrDEL = regexp.MustCompile(`[\s\x7f]`)
var controlOrSpace = regexp.MustCompile(`[\x00-\x20\x7f]`)

type ParsedRecord struct {
	Index        int
	Email        string
	RefreshToken string
	Invalid      bool
}

func ParseCockpitDocument(document any) (records []ParsedRecord, code string) {
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > MaxBytes {
		return nil, "invalid_document"
	}
	arr, ok := document.([]any)
	if !ok || len(arr) == 0 || len(arr) > MaxRecords {
		return nil, "invalid_document"
	}
	seen := map[string]bool{}
	out := make([]ParsedRecord, 0, len(arr))
	for i, item := range arr {
		row, ok := item.(map[string]any)
		if !ok {
			out = append(out, ParsedRecord{Index: i, Invalid: true})
			continue
		}
		for key := range row {
			switch key {
			case "email", "refresh_token", "tags", "notes":
			default:
				ok = false
			}
		}
		if !ok {
			out = append(out, ParsedRecord{Index: i, Invalid: true})
			continue
		}
		email, emailOK := validEmail(row["email"])
		token, tokenOK := validRefresh(row["refresh_token"])
		if !emailOK || !tokenOK || !validMeta(row) {
			out = append(out, ParsedRecord{Index: i, Invalid: true})
			continue
		}
		if seen[email] {
			out = append(out, ParsedRecord{Index: i, Invalid: true})
			continue
		}
		seen[email] = true
		out = append(out, ParsedRecord{Index: i, Email: email, RefreshToken: token})
	}
	return out, ""
}

func validEmail(value any) (string, bool) {
	s, ok := value.(string)
	if !ok || s == "" || utf8.RuneCountInString(s) > MaxEmailLength {
		return "", false
	}
	if s != strings.TrimSpace(s) || controlOrSpace.MatchString(s) {
		return "", false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') || at >= len(s)-1 {
		return "", false
	}
	return strings.ToLower(s), true
}

func validRefresh(value any) (string, bool) {
	s, ok := value.(string)
	if !ok || s == "" || len(s) > MaxRefreshTokenBytes || whitespaceOrDEL.MatchString(s) {
		return "", false
	}
	return s, true
}

func validMeta(row map[string]any) bool {
	if tags, ok := row["tags"]; ok {
		list, ok := tags.([]any)
		if !ok {
			return false
		}
		for _, tag := range list {
			if _, ok := tag.(string); !ok {
				return false
			}
		}
	}
	if notes, ok := row["notes"]; ok {
		if _, ok := notes.(string); !ok {
			return false
		}
	}
	return true
}
