package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

var accessDo = defaultAccessDo

func defaultAccessDo(method, url string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, payload, nil
}

func liveProxyBase(deps commandDependencies) (string, error) {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return "", err
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	if err != nil || state.PID <= 0 || state.Port <= 0 {
		return "", fmt.Errorf("proxy not running")
	}
	host := strings.TrimSpace(state.Hostname)
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, state.Port), nil
}

func runAccessModels(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1
	}
	code, payload, err := accessDo(http.MethodGet, base+"/v1/models", nil)
	if err != nil {
		fmt.Fprintf(stderr, "benes: access models: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: access models failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(payload)
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	var envelope struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		fmt.Fprintln(stderr, "benes: access models returned an unexpected payload")
		return 1
	}
	for _, row := range envelope.Data {
		fmt.Fprintf(stdout, "%s  %s\n", row.ID, row.OwnedBy)
	}
	return 0
}

func runAccessTest(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	protocol := "chat"
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--protocol" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: --protocol must be chat, responses, or messages")
				return 2
			}
			protocol = args[i+1]
			i++
			continue
		}
		filtered = append(filtered, args[i])
	}
	if len(filtered) != 1 {
		fmt.Fprintln(stderr, "benes: usage: access test <model> [--protocol chat|responses|messages]")
		return 2
	}
	if protocol != "chat" && protocol != "responses" && protocol != "messages" {
		fmt.Fprintln(stderr, "benes: --protocol must be chat, responses, or messages")
		return 2
	}
	model := filtered[0]
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1
	}
	path := "/v1/chat/completions"
	var body []byte
	switch protocol {
	case "responses":
		path = "/v1/responses"
		body, _ = json.Marshal(map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 16})
	case "messages":
		path = "/v1/messages"
		body, _ = json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "max_tokens": 16})
	default:
		body, _ = json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "max_tokens": 16, "stream": false})
	}
	code, payload, err := accessDo(http.MethodPost, base+path, body)
	if err != nil {
		fmt.Fprintf(stderr, "benes: access test: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: access test failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(payload)
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	fmt.Fprintf(stdout, "%s: %s request succeeded.\n", model, protocol)
	return 0
}
