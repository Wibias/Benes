package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/Wibias/Benes/internal/export"
)

func runExport(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	args, force := takeConfigFlag(args, "--force")
	client, args := takeOption(args, "--client")
	out, args := takeOption(args, "--out")
	if client == "" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		client = args[0]
		args = args[1:]
	}
	if len(args) > 0 || client == "" {
		fmt.Fprintf(stderr, "benes: usage: export --client <%s> [--json] [--out path] [--force]\n", strings.Join(export.ClientIDs, "|"))
		return 2
	}
	code, payload, err := observeGET("/api/client-config?client="+url.QueryEscape(client), stderr, deps)
	if err != nil {
		return 1
	}
	if code != 0 {
		return code
	}
	var body struct {
		Text                string `json:"text"`
		Destination         string `json:"destination"`
		ExportHint          string `json:"exportHint"`
		ModelCount          int    `json:"modelCount"`
		ModelsWithoutLimits int    `json:"modelsWithoutLimits"`
		Config              any    `json:"config"`
	}
	if json.Unmarshal(payload, &body) != nil {
		fmt.Fprintln(stderr, "benes: unexpected export payload")
		return 1
	}
	if strings.Contains(body.Text, "sk-") {
		fmt.Fprintln(stderr, "benes: export refused to emit a secret")
		return 1
	}
	if out != "" {
		flag := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if force {
			flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		}
		file, err := os.OpenFile(out, flag, 0o600)
		if err != nil {
			if os.IsExist(err) {
				fmt.Fprintf(stderr, "benes: %s already exists. Re-run with --force to replace it.\n", out)
				return 2
			}
			return serveFailure(stderr, "write export", err)
		}
		_, err = file.WriteString(body.Text)
		_ = file.Close()
		if err != nil {
			return serveFailure(stderr, "write export", err)
		}
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, body.Config)
	}
	fmt.Fprintln(stdout, strings.TrimRight(body.Text, "\n"))
	fmt.Fprintln(stdout)
	if out != "" {
		fmt.Fprintf(stdout, "Wrote %s\n", out)
	}
	fmt.Fprintf(stdout, "Destination: %s\n", body.Destination)
	fmt.Fprintln(stdout, "Merge this provider block into that file; do not replace it.")
	fmt.Fprintf(stdout, "Before launching: %s\n", body.ExportHint)
	fmt.Fprintf(stdout, "%d models; %d omit context limits (the client applies its own defaults).\n", body.ModelCount, body.ModelsWithoutLimits)
	return 0
}
