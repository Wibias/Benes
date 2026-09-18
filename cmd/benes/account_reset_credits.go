package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/codexauth"
)

var newRedeemRequestID = codexauth.NewRedeemRequestID

func runAccountResetCredits(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, consume := takeConfigFlag(args, "--consume")
	args, yes := takeConfigFlag(args, "--yes")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: account reset-credits <account-id|main> [--consume --yes] [--json]")
		return 2
	}
	if consume && !yes {
		fmt.Fprintln(stderr, "benes: consuming a reset credit requires --yes")
		return 2
	}
	id := strings.TrimSpace(args[0])
	if id == "main" {
		id = "__main__"
	}
	if consume {
		redeemRequestID, err := newRedeemRequestID()
		if err != nil {
			fmt.Fprintf(stderr, "benes: reset-credits failed: %v\n", err)
			return 1
		}
		payload, err := json.Marshal(struct {
			AccountID       string `json:"accountId"`
			RedeemRequestID string `json:"redeemRequestId"`
		}{AccountID: id, RedeemRequestID: redeemRequestID})
		if err != nil {
			fmt.Fprintf(stderr, "benes: reset-credits failed: %v\n", err)
			return 1
		}
		status, raw, fail := accountLiveAccess("POST", "/api/codex-auth/reset-credits/consume", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintf(stderr, "benes: reset-credits failed: HTTP %d\n", status)
			return 1
		}
		if jsonOut {
			_, _ = stdout.Write(raw)
			if len(raw) == 0 || raw[len(raw)-1] != '\n' {
				fmt.Fprintln(stdout)
			}
			return 0
		}
		fmt.Fprintln(stdout, string(raw))
		return 0
	}
	status, raw, fail := accountLiveAccess("GET", "/api/codex-auth/reset-credits?accountId="+id, nil, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: reset-credits failed: HTTP %d\n", status)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(raw)
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	fmt.Fprintln(stdout, string(raw))
	return 0
}
