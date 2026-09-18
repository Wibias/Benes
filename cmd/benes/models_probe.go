package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/modelprobe"
)

func runModelsProbe(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, generate := takeConfigFlag(args, "--generate")
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(stderr, "benes: usage: models probe <provider> [model] [--generate] [--json]")
		return 2
	}
	provider := strings.TrimSpace(args[0])
	model := ""
	if len(args) == 2 {
		model = strings.TrimSpace(args[1])
	}
	if provider == "" {
		fmt.Fprintln(stderr, "benes: usage: models probe <provider> [model] [--generate] [--json]")
		return 2
	}
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1
	}
	payload := map[string]any{"provider": provider, "generate": generate}
	if model != "" {
		payload["model"] = model
	} else {
		fmt.Fprintln(stderr, "benes: usage: models probe <provider> <model> [--generate] [--json]")
		return 2
	}
	if generate {
		fmt.Fprintln(stderr, "benes: generation probe may consume quota; default catalog probe does not generate")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(stderr, "benes: models probe: %v\n", err)
		return 1
	}
	code, raw, err := accessDo(http.MethodPost, strings.TrimRight(base, "/")+"/api/models/probe", body)
	if err != nil {
		fmt.Fprintf(stderr, "benes: models probe: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: models probe failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(raw)
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	var result modelprobe.Result
	if json.Unmarshal(raw, &result) != nil {
		fmt.Fprintln(stderr, "benes: models probe returned an unexpected payload")
		return 1
	}
	fmt.Fprintf(stdout, "%s/%s: %s\n", result.Provider, result.Model, result.State)
	if result.Reason != "" {
		fmt.Fprintf(stdout, "reason: %s\n", result.Reason)
	}
	fmt.Fprintf(stdout, "kind: %s\n", result.Kind)
	fmt.Fprintf(stdout, "generationIncurred: %t\n", result.GenerationIncurred)
	fmt.Fprintf(stdout, "totalMs: %d\n", result.Timing.TotalMs)
	if result.Timing.HeadersMs != nil {
		fmt.Fprintf(stdout, "headersMs: %d\n", *result.Timing.HeadersMs)
	}
	if result.Timing.FirstOutputMs != nil {
		fmt.Fprintf(stdout, "firstOutputMs: %d\n", *result.Timing.FirstOutputMs)
	}
	if result.State != modelprobe.StateAvailable {
		return 1
	}
	return 0
}
