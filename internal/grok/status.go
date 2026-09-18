package grok

import (
	"path/filepath"
	"strconv"
	"strings"
)

type StatusModel struct {
	Alias         string `json:"alias"`
	ID            string `json:"id"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

type Status struct {
	ConfigPath string        `json:"configPath"`
	Present    bool          `json:"present"`
	BaseURL    string        `json:"baseUrl"`
	Models     []StatusModel `json:"models"`
}

func ReadStatus(opts InjectOptions) Status {
	home := ResolveHome(opts.GrokHome)
	configPath := filepath.Join(home, grokConfigName)
	status := Status{ConfigPath: configPath, Models: []StatusModel{}}
	raw, err := readFile(configPath)
	if err != nil {
		return status
	}
	region := findManagedRegion(raw)
	if region == nil || region.orphaned {
		return status
	}
	status.Present = true
	block := raw[region.start:region.end]
	var current StatusModel
	flush := func() {
		if current.Alias == "" && current.ID == "" {
			return
		}
		status.Models = append(status.Models, current)
		current = StatusModel{}
	}
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[model.") && strings.HasSuffix(trimmed, "]") {
			flush()
			current.Alias = strings.TrimSuffix(strings.TrimPrefix(trimmed, "[model."), "]")
			continue
		}
		if key, value, ok := tomlQuoted(trimmed); ok {
			switch key {
			case "model":
				current.ID = value
			case "base_url":
				if status.BaseURL == "" {
					status.BaseURL = value
				}
			}
			continue
		}
		if key, n, ok := tomlInt(trimmed); ok && key == "context_window" {
			current.ContextWindow = n
		}
	}
	flush()
	return status
}

func tomlQuoted(line string) (string, string, bool) {
	eq := strings.Index(line, "=")
	if eq <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:eq])
	raw := strings.TrimSpace(line[eq+1:])
	if len(raw) < 2 || raw[0] != '"' {
		return "", "", false
	}
	end := strings.LastIndex(raw, `"`)
	if end <= 0 {
		return "", "", false
	}
	return key, raw[1:end], true
}

func tomlInt(line string) (string, int, bool) {
	eq := strings.Index(line, "=")
	if eq <= 0 {
		return "", 0, false
	}
	key := strings.TrimSpace(line[:eq])
	n, err := strconv.Atoi(strings.TrimSpace(line[eq+1:]))
	if err != nil {
		return "", 0, false
	}
	return key, n, true
}
