package grok

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Wibias/Benes/internal/harnessidentity"
)

const (
	BeginMarker    = "# >>> benes managed block — do not edit (removed by `benes stop`) >>>"
	EndMarker      = "# <<< benes managed block <<<"
	loopbackKey    = "benes-loopback"
	grokConfigName = "config.toml"
)

type Model struct {
	ID            string
	Name          string
	ContextWindow int
}

type Result struct {
	OK            bool
	Changed       bool
	Message       string
	SkippedReason string
}

type InjectOptions struct {
	GrokHome string
	Hostname string
}

func ResolveHome(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if home := strings.TrimSpace(os.Getenv("GROK_HOME")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".grok")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// HomeExists reports whether the resolved Grok home directory exists. The native
// integration list uses it to tell "Grok Build is not set up on this machine" apart from
// "Grok Build is set up and Benes has not written its managed block yet".
func HomeExists(opts InjectOptions) bool {
	return isDir(ResolveHome(opts.GrokHome))
}

func isLoopback(hostname string) bool {
	switch strings.ToLower(strings.TrimSpace(hostname)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

type region struct {
	start    int
	end      int
	orphaned bool
}

func findManagedRegion(content string) *region {
	start := strings.Index(content, BeginMarker)
	if start < 0 {
		return nil
	}
	endStart := strings.Index(content[start+len(BeginMarker):], EndMarker)
	if endStart < 0 {
		return &region{start: start, end: len(content), orphaned: true}
	}
	endStart += start + len(BeginMarker)
	return &region{start: start, end: endStart + len(EndMarker), orphaned: false}
}

var aliasUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// BuildManagedBlock renders the one region Benes owns in the Grok config. Every model in
// the canonical catalogue it is handed is written: the block is a projection of the Benes
// model set, not a second place where that set is decided.
func BuildManagedBlock(port int, models []Model, hostname string) string {
	host := strings.TrimSpace(hostname)
	if host == "" {
		host = "127.0.0.1"
	}
	baseURL := fmt.Sprintf("http://%s:%d/v1", host, port)
	lines := []string{BeginMarker}
	aliasCounts := map[string]int{}
	taken := map[string]struct{}{}
	for _, model := range models {
		baseAlias := "benes-" + aliasUnsafe.ReplaceAllString(model.ID, "-")
		count := aliasCounts[baseAlias] + 1
		alias := baseAlias
		if count > 1 {
			alias = fmt.Sprintf("%s-%d", baseAlias, count)
		}
		for {
			if _, exists := taken[alias]; !exists {
				break
			}
			count++
			alias = fmt.Sprintf("%s-%d", baseAlias, count)
		}
		aliasCounts[baseAlias] = count
		taken[alias] = struct{}{}
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		name := model.Name
		if name == "" {
			name = "benes " + model.ID
		}
		lines = append(lines,
			fmt.Sprintf("[model.%s]", alias),
			fmt.Sprintf("model = %q", model.ID),
			fmt.Sprintf("base_url = %q", baseURL),
			`api_backend = "chat_completions"`,
			fmt.Sprintf("api_key = %q", loopbackKey),
			fmt.Sprintf("name = %q", name),
			fmt.Sprintf(`extra_headers = { "x-benes-grok" = "1", "X-Benes-Surface" = "grok", %q = "grok" }`, harnessidentity.Header),
		)
		if model.ContextWindow > 0 {
			lines = append(lines, fmt.Sprintf("context_window = %d", model.ContextWindow))
		}
	}
	lines = append(lines, EndMarker)
	return strings.Join(lines, "\n")
}

func Inject(port int, models []Model, opts InjectOptions) Result {
	home := ResolveHome(opts.GrokHome)
	if !isDir(home) {
		return Result{OK: true, Message: fmt.Sprintf("Grok home not found at %s; config injection skipped.", home), SkippedReason: "no-grok-home"}
	}
	host := strings.TrimSpace(opts.Hostname)
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopback(host) {
		removed := Strip(InjectOptions{GrokHome: opts.GrokHome})
		cleanup := ""
		if removed.Changed {
			cleanup = " Removed the previously generated block, which pointed at a loopback address."
		}
		return Result{
			OK:            true,
			Changed:       removed.Changed,
			SkippedReason: "non-loopback",
			Message:       `Grok auto-registration skipped: benes is bound to the non-loopback host "` + host + `".` + cleanup,
		}
	}
	configPath := filepath.Join(home, grokConfigName)
	raw, err := readFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return Result{OK: false, Message: fmt.Sprintf("Grok inject failed: %v", err)}
		}
		raw = ""
	}
	region := findManagedRegion(raw)
	if region != nil && region.orphaned {
		return Result{OK: false, Message: "Grok config has an orphaned benes marker; injection skipped."}
	}
	block := BuildManagedBlock(port, models, host)
	var next string
	if region != nil {
		next = raw[:region.start] + block + raw[region.end:]
	} else if raw == "" {
		next = block + "\n"
	} else {
		next = strings.TrimRight(raw, "\n") + "\n" + block + "\n"
	}
	if next == raw {
		return Result{OK: true, Message: "Grok config already contains the current benes managed block."}
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("Grok inject failed: %v", err)}
	}
	if err := writeFile(configPath, next); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("Grok inject failed: %v", err)}
	}
	if region != nil {
		return Result{OK: true, Changed: true, Message: "Updated the benes managed block in Grok config."}
	}
	return Result{OK: true, Changed: true, Message: "Added the benes managed block to Grok config."}
}

func Strip(opts InjectOptions) Result {
	home := ResolveHome(opts.GrokHome)
	if !isDir(home) {
		return Result{OK: true, Message: fmt.Sprintf("Grok home not found at %s; no managed config to remove.", home), SkippedReason: "no-grok-home"}
	}
	configPath := filepath.Join(home, grokConfigName)
	content, err := readFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{OK: true, Message: "Grok config not found; no managed block to remove."}
		}
		return Result{OK: false, Message: fmt.Sprintf("Grok strip failed: %v", err)}
	}
	region := findManagedRegion(content)
	if region == nil {
		return Result{OK: true, Message: "No benes managed block found in Grok config."}
	}
	if region.orphaned {
		return Result{OK: false, Message: "Grok config has an orphaned benes marker; cleanup skipped."}
	}
	removalEnd := region.end
	if removalEnd < len(content) && content[removalEnd] == '\n' {
		removalEnd++
	}
	prefix := content[:region.start]
	rest := content[removalEnd:]
	if strings.HasSuffix(prefix, "\n\n") {
		prefix = prefix[:len(prefix)-1]
	} else if rest == "" && strings.HasSuffix(prefix, "\n") {
		prefix = prefix[:len(prefix)-1]
	}
	stripped := prefix + rest
	if err := writeFile(configPath, stripped); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("Grok strip failed: %v", err)}
	}
	return Result{OK: true, Changed: true, Message: "Removed the benes managed block from Grok config."}
}
