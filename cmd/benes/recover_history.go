package main

import (
	"fmt"
	"io"

	"github.com/Wibias/Benes/internal/codexhistory"
	"github.com/Wibias/Benes/internal/config"
)

func runRecoverHistory(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "Usage: benes recover-history --legacy-openai")
		fmt.Fprintln(stdout, "Only use this if an older syncResumeHistory build already remapped OpenAI Codex App history to benes before backup support existed.")
		return 0
	}
	if len(args) != 1 || args[0] != "--legacy-openai" {
		fmt.Fprintln(stderr, "Usage: benes recover-history --legacy-openai")
		fmt.Fprintln(stderr, "Only use this if an older syncResumeHistory build already remapped OpenAI Codex App history to benes before backup support existed.")
		return 1
	}

	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve CODEX_HOME: %v\n", err)
		return 1
	}
	result, err := codexhistory.RecoverLegacyOpenAI(home)
	if err != nil {
		fmt.Fprintf(stderr, "⚠️  Recovery SKIPPED: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Recovered %d legacy thread(s) to openai (%d rollout file(s) updated).\n", result.Rows, result.Files)
	return 0
}
