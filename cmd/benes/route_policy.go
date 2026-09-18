package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type routingProfile struct {
	Candidates []map[string]any `json:"candidates"`
	Alias      string           `json:"alias,omitempty"`
}

func runRoutePolicy(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: route policy list|show|dry-run|evaluate")
		return 2
	}
	switch args[0] {
	case "list":
		return runRoutePolicyList(jsonOut, stdout, stderr, deps)
	case "show":
		return runRoutePolicyShow(args[1:], jsonOut, stdout, stderr, deps)
	case "dry-run", "evaluate":
		return runRoutePolicyDryRun(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: route policy list|show|dry-run|evaluate")
		return 2
	}

}

func runRoutePolicyList(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	profiles, err := loadRoutingProfiles(deps)
	if err != nil {
		return serveFailure(stderr, "load routing profiles", err)
	}
	if jsonOut {
		rows := make([]map[string]any, 0, len(profiles))
		for id := range profiles {
			rows = append(rows, map[string]any{"id": id, "model": "policy/" + id})
		}
		return encodeJSON(stdout, stderr, map[string]any{"profiles": rows})
	}
	if len(profiles) == 0 {
		fmt.Fprintln(stdout, "No routing profiles configured.")
		return 0
	}
	for _, id := range sortedKeys(profiles) {
		fmt.Fprintf(stdout, "%s  policy/%s\n", id, id)
	}
	return 0
}

func runRoutePolicyShow(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: route policy show <id>")
		return 2
	}
	profiles, err := loadRoutingProfiles(deps)
	if err != nil {
		return serveFailure(stderr, "load routing profiles", err)
	}
	profile, ok := profiles[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "benes: unknown routing profile: %s\n", args[0])
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, profile)
	}
	fmt.Fprintf(stdout, "id: %s\n", args[0])
	if profile.Alias != "" {
		fmt.Fprintf(stdout, "alias: %s\n", profile.Alias)
	}
	for _, candidate := range profile.Candidates {
		provider, _ := candidate["provider"].(string)
		model, _ := candidate["model"].(string)
		fmt.Fprintf(stdout, "candidate: %s/%s\n", provider, model)
	}
	return 0
}

func runRoutePolicyDryRun(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, tools := takeConfigFlag(args, "--tools")
	args, image := takeConfigFlag(args, "--image")
	args, structured := takeConfigFlag(args, "--structured-output")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: route policy dry-run <id> [--tools] [--image] [--structured-output] [--json]")
		return 2
	}
	profiles, err := loadRoutingProfiles(deps)
	if err != nil {
		return serveFailure(stderr, "load routing profiles", err)
	}
	profile, ok := profiles[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "benes: unknown routing profile: %s\n", args[0])
		return 1
	}
	var selected map[string]any
	if len(profile.Candidates) > 0 {
		selected = profile.Candidates[0]
	}
	view := map[string]any{
		"profile":    args[0],
		"mode":       "selection-only",
		"selected":   selected,
		"candidates": profile.Candidates,
		"evidence": map[string]any{
			"toolsRequired":            tools,
			"imageInputRequired":       image,
			"structuredOutputRequired": structured,
		},
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, view)
	}
	if selected == nil {
		fmt.Fprintln(stdout, "selected: none")
		return 0
	}
	fmt.Fprintf(stdout, "selected: %s/%s\n", observeString(selected["provider"]), observeString(selected["model"]))
	fmt.Fprintln(stdout, "mode: selection-only")
	return 0
}

func loadRoutingProfiles(deps commandDependencies) (map[string]routingProfile, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	raw, ok := root["routingProfiles"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]routingProfile{}, nil
	}
	var profiles map[string]routingProfile
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, err
	}
	if profiles == nil {
		return map[string]routingProfile{}, nil
	}
	return profiles, nil
}

func runAccessEndpoints(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	host := "127.0.0.1"
	port := 23100
	if raw, ok := root["hostname"]; ok {
		_ = json.Unmarshal(raw, &host)
	}
	if raw, ok := root["port"]; ok {
		_ = json.Unmarshal(raw, &port)
	}
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if port <= 0 || port > 65535 {
		port = 23100
	}
	base := fmt.Sprintf("http://%s:%d", host, port)
	view := map[string]string{
		"baseUrl":           base,
		"openaiEndpoint":    base + "/v1",
		"anthropicEndpoint": base + "/anthropic",
		"responsesEndpoint": base + "/v1/responses",
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, view)
	}
	for _, key := range []string{"baseUrl", "openaiEndpoint", "anthropicEndpoint", "responsesEndpoint"} {
		fmt.Fprintf(stdout, "%s: %s\n", key, view[key])
	}
	return 0
}
