package harnessboard

import "github.com/Wibias/Benes/internal/export"

// IDs is the harness board order. Native clients sit beside the file-integration
// clients; keep this list in lockstep with the dashboard HarnessId union.
var IDs = []string{
	"claude-desktop",
	"claude",
	"codex",
	"dsh",
	"opencode",
	"pi",
	"prime",
	"omp",
	"hermes",
	"openclaw",
	"kimi",
	"gajae",
	"grok",
	"mcode",
}

func Known(id string) bool {
	for _, item := range IDs {
		if item == id {
			return true
		}
	}
	return false
}

func fileClient(id string) bool {
	for _, item := range export.ClientIDs {
		if item == id {
			return true
		}
	}
	return false
}

func processNames(id string) []string {
	switch id {
	case "claude-desktop":
		return []string{"Claude", "claude"}
	case "claude":
		return []string{"claude"}
	case "codex":
		return []string{"codex", "Codex"}
	case "grok":
		return []string{"Grok", "grok"}
	case "opencode":
		return []string{"opencode"}
	case "pi":
		return []string{"pi"}
	case "prime":
		return []string{"prime"}
	case "omp":
		return []string{"omp"}
	case "hermes":
		return []string{"hermes"}
	case "openclaw":
		return []string{"openclaw"}
	case "kimi":
		return []string{"kimi", "kimi-code"}
	case "gajae":
		return []string{"gajae"}
	case "dsh":
		return []string{"dsh"}
	case "mcode":
		return []string{"mcode"}
	default:
		return nil
	}
}

func lookNames(id string) []string {
	switch id {
	case "claude-desktop":
		return []string{"Claude", "claude"}
	case "claude":
		return []string{"claude"}
	case "codex":
		return []string{"codex"}
	case "grok":
		return []string{"grok"}
	default:
		if fileClient(id) {
			return []string{id}
		}
		return nil
	}
}
