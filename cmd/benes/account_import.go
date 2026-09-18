package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const accountImportMaxBytes = 256 * 1024

func runAccountImport(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	format, args := takeFlagValue(args, "--format")
	filePath, args := takeFlagValue(args, "--file")
	args, fromStdin := takeConfigFlag(args, "--stdin")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Error: unsupported_provider")
		return 1
	}
	provider := args[0]
	if provider != "google-antigravity" {
		fmt.Fprintln(stderr, "Error: unsupported_provider")
		return 1
	}
	if format != "cockpit-tools" {
		fmt.Fprintln(stderr, "Error: unsupported_format")
		return 1
	}
	hasFile := strings.TrimSpace(filePath) != ""
	if hasFile == fromStdin {
		fmt.Fprintln(stderr, "Error: choose exactly one bounded source: --file <path> or --stdin")
		return 1
	}
	var source string
	var err error
	if hasFile {
		source, err = readBoundedImportFile(filePath)
	} else {
		source, err = readAllBounded(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err.Error())
		return 1
	}
	var document any
	if json.Unmarshal([]byte(source), &document) != nil {
		fmt.Fprintln(stderr, "Error: invalid_document")
		return 1
	}
	payload, err := json.Marshal(map[string]any{
		"provider": provider,
		"format":   format,
		"document": document,
	})
	if err != nil {
		fmt.Fprintln(stderr, "Error: invalid_document")
		return 1
	}
	status, raw, fail := accountLiveAccess("POST", "/api/oauth/accounts/import", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		var failBody struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &failBody)
		if failBody.Code != "" {
			fmt.Fprintf(stderr, "Error: %s\n", failBody.Code)
			return 1
		}
		fmt.Fprintf(stderr, "Error: import_request_failed (status %d)\n", status)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(raw)
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	var result struct {
		ImportedCount    int `json:"importedCount"`
		UpdatedCount     int `json:"updatedCount"`
		FailedCount      int `json:"failedCount"`
		UnsupportedCount int `json:"unsupportedCount"`
		Results          []struct {
			Index  int    `json:"index"`
			Status string `json:"status"`
			Code   string `json:"code"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &result) != nil {
		fmt.Fprintln(stderr, "Error: invalid_response")
		return 1
	}
	fmt.Fprintf(stdout, "%s: %d imported, %d updated, %d failed, %d unsupported\n",
		provider, result.ImportedCount, result.UpdatedCount, result.FailedCount, result.UnsupportedCount)
	for _, item := range result.Results {
		fmt.Fprintf(stdout, "  #%d %s (%s)\n", item.Index+1, item.Status, item.Code)
	}
	if result.FailedCount > 0 || result.UnsupportedCount > 0 {
		return 1
	}
	return 0
}

func takeFlagValue(args []string, name string) (string, []string) {
	out := make([]string, 0, len(args))
	value := ""
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			value = args[i+1]
			i++
			continue
		}
		out = append(out, args[i])
	}
	return value, out
}

func readBoundedImportFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("source_read_failed")
	}
	if len(raw) > accountImportMaxBytes || !utf8.Valid(raw) {
		return "", fmt.Errorf("invalid_document")
	}
	return string(raw), nil
}

func readAllBounded(r io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, accountImportMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("source_read_failed")
	}
	if len(raw) > accountImportMaxBytes || !utf8.Valid(raw) {
		return "", fmt.Errorf("invalid_document")
	}
	return string(raw), nil
}
