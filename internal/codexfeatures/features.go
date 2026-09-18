package codexfeatures

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func Enabled(home string) (bool, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return false, nil
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return EnabledFromText(string(raw)), nil
}

var (
	tomlEnabledAssign = regexp.MustCompile(`(?m)^\s*enabled\s*=\s*(true|false)\b`)
	tomlV2BoolAssign  = regexp.MustCompile(`(?m)^\s*multi_agent_v2\s*=\s*(true|false)\b`)
	tomlV2InlineTable = regexp.MustCompile(`(?m)^\s*multi_agent_v2\s*=\s*\{([^}]*)\}`)
	tomlInlineEnabled = regexp.MustCompile(`enabled\s*=\s*(true|false)`)
	tomlSectionHeader = regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]\s*$`)
)

func EnabledFromText(content string) bool {
	if body, ok := tomlTableBody(content, "features.multi_agent_v2"); ok {
		if m := tomlEnabledAssign.FindStringSubmatch(body); len(m) == 2 {
			return m[1] == "true"
		}
		return false
	}
	body, ok := tomlTableBody(content, "features")
	if !ok {
		return false
	}
	if m := tomlV2BoolAssign.FindStringSubmatch(body); len(m) == 2 {
		return m[1] == "true"
	}
	if m := tomlV2InlineTable.FindStringSubmatch(body); len(m) == 2 {
		if e := tomlInlineEnabled.FindStringSubmatch(m[1]); len(e) == 2 {
			return e[1] == "true"
		}
	}
	return false
}

func tomlTableBody(content, name string) (string, bool) {
	matches := tomlSectionHeader.FindAllStringSubmatchIndex(content, -1)
	for i, loc := range matches {
		header := strings.TrimSpace(content[loc[2]:loc[3]])
		if header != name {
			continue
		}
		start := loc[1]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		return content[start:end], true
	}
	return "", false
}
