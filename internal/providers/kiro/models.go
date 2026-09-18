package kiro

import (
	"regexp"
	"strings"
)

var (
	dateSuffix    = regexp.MustCompile(`-\d{8}$`)
	effortSuffix  = regexp.MustCompile(`-(low|medium|high|xhigh|max)$`)
	dottedVersion = regexp.MustCompile(`(\d+)-(\d+)`)
	claudeFlip    = regexp.MustCompile(`^claude-([\d.]+)-(sonnet|opus|haiku)$`)
)

var kiroContextWindows = map[string]int64{
	"gpt-5.6-sol":       272_000,
	"gpt-5.6-terra":     272_000,
	"gpt-5.6-luna":      272_000,
	"claude-sonnet-5":   1_000_000,
	"claude-opus-5":     1_000_000,
	"claude-opus-4.8":   1_000_000,
	"claude-opus-4.7":   1_000_000,
	"claude-opus-4.6":   1_000_000,
	"claude-opus-4.5":   200_000,
	"claude-sonnet-4.6": 1_000_000,
	"claude-sonnet-4.5": 200_000,
	"claude-sonnet-4.0": 200_000,
	"claude-haiku-4.5":  200_000,
	"deepseek-3.2":      128_000,
	"minimax-m2.5":      200_000,
	"minimax-m2.1":      200_000,
	"glm-5":             200_000,
	"qwen3-coder-next":  256_000,
}

func NormalizeModelID(id string) string {
	model := strings.ToLower(strings.TrimSpace(id))
	model = strings.TrimPrefix(model, "kiro/")
	model = strings.TrimPrefix(model, "kiro-")
	if model == "auto" {
		return "auto"
	}
	model = dateSuffix.ReplaceAllString(model, "")
	model = effortSuffix.ReplaceAllString(model, "")
	model = dottedVersion.ReplaceAllString(model, "$1.$2")
	if match := claudeFlip.FindStringSubmatch(model); len(match) == 3 {
		model = "claude-" + match[2] + "-" + match[1]
	}
	return model
}

func ContextWindow(id string) int64 {
	return kiroContextWindows[NormalizeModelID(id)]
}
