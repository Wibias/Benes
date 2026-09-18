package modelpreset

import (
	"regexp"
	"sort"
)

type Spec struct {
	Version  int
	Patterns []*regexp.Regexp
}

func Lookup(provider string) (Spec, bool) {
	spec, ok := registry[provider]
	return spec, ok
}

func Match(catalogIDs []string, spec Spec) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, id := range catalogIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if !matchesAny(id, spec.Patterns) {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func matchesAny(id string, patterns []*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(id) {
			return true
		}
	}
	return false
}

func compile(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, regexp.MustCompile(pattern))
	}
	return out
}

// OpenRouter core ids are the current Discover flagships plus Benes-used
// OpenRouter-style slugs (claude-sonnet-4 in router tests, grok-4.6 and
// deepseek-v4-flash in usage tests). Historical snapshots and :free rows stay out.
var registry = map[string]Spec{
	"openrouter": {
		Version: 1,
		Patterns: compile(
			`^anthropic/claude-(opus|sonnet)-5($|[-.])`,
			`^openai/gpt-5\.6-(sol|terra|luna)($|-)`,
			`^google/gemini-3(\.|-)`,
			`^x-ai/grok-4\.6($|[-.])`,
			`^deepseek/deepseek-v4($|[-.])`,
		),
	},
}
