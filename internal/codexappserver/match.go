package codexappserver

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Rust-style target-triple body on official platform-baked Codex binaries
// (e.g. x86_64-unknown-linux-musl, aarch64-apple-darwin, x86_64-pc-windows-msvc).
const targetTripleBody = `[a-z0-9_]+-[a-z0-9_]+-[a-z0-9_]+(?:-[a-z0-9_]+)?`

var (
	windowsBasenameCandidateRe = regexp.MustCompile(
		`(?i)(^|[/\\\s'"=])codex(-` + targetTripleBody + `)?([.]exe|[.]cmd)?['"]?(\s|$)`,
	)
	windowsCodeModeHostCandidateRe = regexp.MustCompile(`(?i)codex-code-mode-host`)
	codexTargetTripleBasenameRe    = regexp.MustCompile(`^codex-` + targetTripleBody + `(?:\.exe|\.cmd)?$`)
)

var codexGlobalOptionsWithValue = map[string]struct{}{
	"--enable": {}, "--disable": {}, "--config": {}, "-c": {},
	"--profile": {}, "-p": {}, "--model": {}, "-m": {},
	"--sandbox": {}, "-s": {}, "--ask-for-approval": {}, "-a": {},
	"--local-provider": {}, "--add-dir": {}, "--cd": {}, "-C": {},
	"--color": {}, "--image": {}, "-i": {}, "--output-schema": {},
	"--output-last-message": {}, "-o": {},
}

// WindowsBasenameCandidateSource is the un-flagged regex source embedded in the
// Windows CIM pre-filter. Tests pin this so the PowerShell -match stays aligned.
const WindowsBasenameCandidateSource = `(^|[/\\\s'"=])codex(-` + targetTripleBody + `)?([.]exe|[.]cmd)?['"]?(\s|$)`

// WindowsCodeModeHostCandidateSource is the un-flagged code-mode-host CIM pre-filter.
const WindowsCodeModeHostCandidateSource = `codex-code-mode-host`

// IsWindowsCodexCandidateCommandLine reports whether a Windows CommandLine is
// worth paying GetOwner for. Stay narrow: incidental "benes" paths must not.
func IsWindowsCodexCandidateCommandLine(commandLine string) bool {
	return windowsBasenameCandidateRe.MatchString(commandLine) ||
		windowsCodeModeHostCandidateRe.MatchString(commandLine)
}

// TokenizeCommandLine splits a process command line into argv-like tokens
// (handles simple quotes).
func TokenizeCommandLine(commandLine string) []string {
	tokens := make([]string, 0, 8)
	var current strings.Builder
	var quote rune
	for _, ch := range commandLine {
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				current.WriteRune(ch)
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if unicode.IsSpace(ch) {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func tokenBasename(token string) string {
	base := strings.ToLower(strings.ReplaceAll(token, "\\", "/"))
	return path.Base(base)
}

func isCodexExecutableToken(token string) bool {
	base := tokenBasename(token)
	return base == "codex" || base == "codex.exe" || base == "codex.cmd" ||
		codexTargetTripleBasenameRe.MatchString(base)
}

func isCodeModeHostToken(token string) bool {
	base := tokenBasename(token)
	return base == "codex-code-mode-host" || base == "codex-code-mode-host.exe"
}

func isInterpreterToken(token string) bool {
	base := tokenBasename(token)
	return base == "node" || base == "node.exe" ||
		base == "deno" || base == "deno.exe"
}

type cliOption struct {
	name           string
	hasInlineValue bool
}

func splitCLIOptionToken(token string) (cliOption, bool) {
	if !strings.HasPrefix(token, "-") || token == "-" || token == "--" {
		return cliOption{}, false
	}
	if strings.HasPrefix(token, "--") {
		if eq := strings.IndexByte(token, '='); eq >= 0 {
			return cliOption{name: strings.ToLower(token[:eq]), hasInlineValue: true}, true
		}
		return cliOption{name: strings.ToLower(token)}, true
	}
	if eq := strings.IndexByte(token, '='); eq >= 0 {
		return cliOption{name: token[:eq], hasInlineValue: true}, true
	}
	return cliOption{name: token}, true
}

func advancePastCodexGlobalOption(tokens []string, index int) int {
	option, ok := splitCLIOptionToken(tokens[index])
	if !ok {
		return index + 1
	}
	next := index + 1
	if !option.hasInlineValue {
		if _, known := codexGlobalOptionsWithValue[option.name]; known && next < len(tokens) && !strings.HasPrefix(tokens[next], "-") {
			next++
		}
	}
	return next
}

func isCodeModeHostProcess(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	if isCodeModeHostToken(tokens[0]) {
		return true
	}
	return isInterpreterToken(tokens[0]) && len(tokens) > 1 && isCodeModeHostToken(tokens[1])
}

// ProcessIdentity is a stable identity for PID reuse checks: pid + normalized command line.
func ProcessIdentity(pid int, commandLine string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(commandLine)), " ")
	return strconv.Itoa(pid) + "\x00" + normalized
}

// IsCodexAppServerCommandLine reports whether the command line is a Codex
// app-server (or code-mode host) worth restarting. Matching is intentionally
// narrow: require app-server as the Codex subcommand, never a later argument.
func IsCodexAppServerCommandLine(commandLine string, executable string) bool {
	trimmed := strings.TrimSpace(commandLine)
	tokens := TokenizeCommandLine(trimmed)
	if executable != "" {
		if isCodeModeHostToken(executable) {
			return true
		}
		if isCodexExecutableToken(executable) {
			remainder := trimmed
			if strings.HasPrefix(remainder, executable) {
				remainder = strings.TrimLeftFunc(remainder[len(executable):], unicode.IsSpace)
			} else if quoted := `"` + executable + `"`; strings.HasPrefix(remainder, quoted) {
				remainder = strings.TrimLeftFunc(remainder[len(quoted):], unicode.IsSpace)
			}
			tokens = append([]string{executable}, TokenizeCommandLine(remainder)...)
		}
	}
	if len(tokens) == 0 {
		return false
	}
	if isCodeModeHostProcess(tokens) {
		return true
	}
	if !isCodexExecutableToken(tokens[0]) {
		return false
	}
	i := 1
	for i < len(tokens) {
		token := tokens[i]
		if strings.HasPrefix(token, "-") {
			i = advancePastCodexGlobalOption(tokens, i)
			continue
		}
		return strings.ToLower(token) == "app-server"
	}
	return false
}
