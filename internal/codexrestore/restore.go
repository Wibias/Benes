package codexrestore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

type journal struct {
	Version             int     `json:"version"`
	OriginalConfig      string  `json:"originalConfig"`
	OriginalProfile     *string `json:"originalProfile"`
	InjectedConfigHash  string  `json:"injectedConfigHash"`
	InjectedProfileHash *string `json:"injectedProfileHash"`
	PID                 int     `json:"pid"`
}

type Result struct {
	ConfigRestored  bool
	ProfileRestored bool
	Stripped        bool
	Message         string
}

func Restore(codexHome string) (Result, error) {
	if strings.TrimSpace(codexHome) == "" {
		return Result{}, fmt.Errorf("CODEX_HOME is required")
	}
	if journaled, ok, err := restoreJournal(codexHome); err != nil {
		return Result{}, err
	} else if ok {
		return journaled, nil
	}
	return stripManaged(codexHome)
}

func restoreJournal(codexHome string) (Result, bool, error) {
	path := journalPath(codexHome)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	var j journal
	if json.Unmarshal(raw, &j) != nil || j.Version != 1 {
		_ = os.Remove(path)
		return Result{}, false, nil
	}
	cfg := configPath(codexHome)
	currentConfig := readString(cfg)
	configUnchanged := j.InjectedConfigHash == "" || sha256Hex(currentConfig) == j.InjectedConfigHash
	prof := profilePath(codexHome)
	currentProfile := optionalFile(prof)
	profileUnchanged := j.InjectedProfileHash == nil || sha256Ptr(currentProfile) == hashPtr(j.InjectedProfileHash)

	result := Result{}
	if configUnchanged {
		decoded, err := base64.StdEncoding.DecodeString(j.OriginalConfig)
		if err != nil {
			return Result{}, false, fmt.Errorf("decode journaled config: %w", err)
		}
		restored := userBaseline(string(decoded))
		if err := atomicfile.Write(cfg, []byte(restored), atomicfile.Options{Mode: 0o600}); err != nil {
			return Result{}, false, err
		}
		result.ConfigRestored = true
	}
	if profileUnchanged {
		if j.OriginalProfile != nil {
			decoded, err := base64.StdEncoding.DecodeString(*j.OriginalProfile)
			if err != nil {
				return Result{}, false, fmt.Errorf("decode journaled profile: %w", err)
			}
			if err := atomicfile.Write(prof, decoded, atomicfile.Options{Mode: 0o600}); err != nil {
				return Result{}, false, err
			}
		} else {
			_ = os.Remove(prof)
		}
		result.ProfileRestored = true
	}
	if result.ConfigRestored && result.ProfileRestored {
		_ = os.Remove(path)
		result.Message = "Codex config restored from benes journal."
		return result, true, nil
	}
	stripped, err := stripManaged(codexHome)
	if err != nil {
		return Result{}, false, err
	}
	stripped.ConfigRestored = result.ConfigRestored
	stripped.ProfileRestored = result.ProfileRestored
	return stripped, true, nil
}

func stripManaged(codexHome string) (Result, error) {
	cfg := configPath(codexHome)
	raw, err := os.ReadFile(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.Remove(profilePath(codexHome))
			return Result{Message: "Codex config not found; no native restore was needed."}, nil
		}
		return Result{}, err
	}
	content := string(raw)
	stripped := stripBenes(content)
	changed := stripped != content
	if changed {
		if err := atomicfile.Write(cfg, []byte(stripped), atomicfile.Options{Mode: 0o600}); err != nil {
			return Result{}, err
		}
	}
	_ = os.Remove(profilePath(codexHome))
	if changed {
		return Result{Stripped: true, Message: "Removed benes routing from Codex config + profile."}, nil
	}
	return Result{Message: "benes not present in Codex config."}, nil
}

var (
	modelProviderLine   = regexp.MustCompile(`(?m)^\s*model_provider\s*=\s*"benes"\s*(?:#.*)?$`)
	benesProviderHeader = regexp.MustCompile(`^\[model_providers\.benes(?:\.[^]]+)?\]\s*(?:#.*)?$`)
)

func isBenesProviderHeaderLine(trimmed string) bool {
	return benesProviderHeader.MatchString(trimmed)
}

func hasBenesProviderTable(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if isBenesProviderHeaderLine(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func hasManagedRouting(content string) bool {
	return hasBenesProviderTable(content) || modelProviderLine.MatchString(content)
}

func stripBenes(content string) string {
	out := removeBenesProviderTables(content)
	out = removeSections(out, "profiles.benes")
	out = modelProviderLine.ReplaceAllString(out, "")
	out = strings.ReplaceAll(out, sectionMarker+"\n", "")
	out = strings.ReplaceAll(out, sectionMarker, "")
	out = regexp.MustCompile(`\n{3,}`).ReplaceAllString(out, "\n\n")
	return strings.TrimRight(out, "\n") + "\n"
}

func userBaseline(content string) string {
	if !hasBenesProviderTable(content) && !modelProviderLine.MatchString(content) {
		return content
	}
	return stripBenes(content)
}

func removeBenesProviderTables(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isBenesProviderHeaderLine(trimmed) {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(trimmed, "[") && !isBenesProviderHeaderLine(trimmed) {
			skip = false
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func removeSections(content, prefix string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	header := regexp.MustCompile(`^\[` + regexp.QuoteMeta(prefix) + `(\.|])`)
	next := regexp.MustCompile(`^\[`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if header.MatchString(trimmed) {
			skip = true
			continue
		}
		if skip && next.MatchString(trimmed) {
			skip = false
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func readString(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(body)
}

func optionalFile(path string) *string {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := string(body)
	return &s
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256Ptr(s *string) string {
	if s == nil {
		return ""
	}
	return sha256Hex(*s)
}

func hashPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
