package codexappserver

import "testing"

func TestIsCodexAppServerCommandLineMatchesAppServerAndCodeModeHost(t *testing.T) {
	trueCases := []string{
		"codex app-server --listen unix:///tmp/codex.sock",
		"/usr/local/bin/codex app-server proxy",
		`C:\Users\a\AppData\codex.exe app-server --listen pipe`,
		`"C:\Program Files\nodejs\codex.exe" app-server`,
		`"C:\Program Files\nodejs\codex.cmd" app-server --listen pipe`,
		"codex --verbose app-server",
		"codex-code-mode-host --session 1",
		"node /opt/codex-code-mode-host --session 1",
	}
	for _, command := range trueCases {
		if !IsCodexAppServerCommandLine(command, "") {
			t.Fatalf("want match: %s", command)
		}
	}
}

func TestIsCodexAppServerCommandLineMatchesTargetTripleBasenames(t *testing.T) {
	trueCases := []string{
		"/opt/codex/codex-x86_64-unknown-linux-musl app-server --listen unix:///tmp/c.sock",
		"/Applications/Codex.app/Contents/Resources/codex-aarch64-apple-darwin app-server",
		`C:\Users\a\.codex\bin\codex-x86_64-pc-windows-msvc.exe app-server --listen pipe`,
		`"C:\Program Files\Codex\codex-aarch64-pc-windows-msvc.exe" app-server`,
		"codex-x86_64-apple-darwin --profile prod app-server",
	}
	for _, command := range trueCases {
		if !IsCodexAppServerCommandLine(command, "") {
			t.Fatalf("want match: %s", command)
		}
	}
}

func TestIsCodexAppServerCommandLineMatchesValueTakingGlobalOptions(t *testing.T) {
	trueCases := []string{
		"codex --enable js_repl app-server",
		"codex --enable=js_repl app-server",
		"codex --disable multi_agent_v2 app-server --listen unix://x",
		"codex --config model=gpt-5.4 app-server",
		"codex -c model=gpt-5.4 app-server",
		"codex --profile production app-server",
		"codex -p production app-server",
		"codex -a never app-server",
		"codex --ask-for-approval on-request app-server",
		"codex --oss --local-provider ollama app-server",
		"codex --add-dir /tmp app-server",
		"codex --enable js_repl --profile prod -c model=gpt-5.4 app-server --listen stdio://",
	}
	for _, command := range trueCases {
		if !IsCodexAppServerCommandLine(command, "") {
			t.Fatalf("want match: %s", command)
		}
	}
}

func TestIsCodexAppServerCommandLineRejectsUnrelatedAndLaterArguments(t *testing.T) {
	falseCases := []string{
		"hermes-codex-bridge-mcp --port 9",
		"hermes-codex-x86_64-unknown-linux-gnu app-server",
		"node ./benes/src/cli/index.ts start",
		"benes app-server",
		"/usr/bin/benes app-server",
		"codex-bridge app-server",
		"codex-helper-tool app-server",
		"codex exec 'hello'",
		`codex exec "debug app-server behavior"`,
		"codex exec debug app-server behavior",
		"codex exec app-server",
		"node worker.js codex app-server",
		"something-app-server-without-codex-bin",
		"node worker.js codex-code-mode-host",
		"bash -c codex-code-mode-host",
	}
	for _, command := range falseCases {
		if IsCodexAppServerCommandLine(command, "") {
			t.Fatalf("want reject: %s", command)
		}
	}
}

func TestIsWindowsCodexCandidateCommandLine(t *testing.T) {
	trueCases := []string{
		`"C:\Program Files\nodejs\codex.exe" app-server`,
		`"C:\Program Files\nodejs\codex.cmd" app-server --listen pipe`,
		`codex.exe" app-server`,
		`codex.cmd' app-server`,
		"codex app-server",
		`"C:\Program Files\Codex\codex-x86_64-pc-windows-msvc.exe" app-server`,
		`C:\Users\a\.codex\bin\codex-aarch64-pc-windows-msvc.exe app-server`,
	}
	for _, command := range trueCases {
		if !IsWindowsCodexCandidateCommandLine(command) {
			t.Fatalf("want candidate: %s", command)
		}
	}
	falseCases := []string{
		`node C:\Users\a\benes\src\cli\index.ts start`,
		"benes app-server",
		"hermes-codex-bridge-mcp",
		"hermes-codex-x86_64-pc-windows-msvc.exe",
		"codex-bridge app-server",
	}
	for _, command := range falseCases {
		if IsWindowsCodexCandidateCommandLine(command) {
			t.Fatalf("want reject candidate: %s", command)
		}
	}
}
