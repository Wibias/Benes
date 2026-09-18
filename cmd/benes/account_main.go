package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runAccountMain(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: account main doctor|list|register|add|switch|recover")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	rest, yes := takeConfigFlag(rest, "--yes")
	rest, rollback := takeConfigFlag(rest, "--rollback")
	switch sub {
	case "doctor", "list":
		if len(rest) > 0 || yes || rollback {
			fmt.Fprintln(stderr, "benes: usage: account main "+sub+" [--json]")
			return 2
		}
		path := "/api/native-main-profiles"
		if sub == "doctor" {
			path = "/api/native-main-profiles/doctor"
		}
		status, raw, fail := accountLiveAccess("GET", path, nil, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			return nativeMainAPIError(raw, stderr, "failed to "+sub+" native profiles")
		}
		if jsonOut || sub == "doctor" {
			_, _ = stdout.Write(raw)
			if len(raw) == 0 || raw[len(raw)-1] != '\n' {
				fmt.Fprintln(stdout)
			}
			return 0
		}
		var result struct {
			Profiles []struct {
				ID, Label, IdentityHint, State string
			} `json:"profiles"`
		}
		_ = json.Unmarshal(raw, &result)
		if len(result.Profiles) == 0 {
			fmt.Fprintln(stdout, "No native main login profiles registered.")
			return 0
		}
		for _, p := range result.Profiles {
			mark := " "
			if p.State == "active" {
				mark = "*"
			}
			fmt.Fprintf(stdout, "%s  %s  %s  %s\n", mark, p.Label, p.ID, p.IdentityHint)
		}
		return 0
	case "register":
		if len(rest) != 1 || yes || rollback {
			fmt.Fprintln(stderr, "benes: usage: account main register <label> [--json]")
			return 2
		}
		payload, _ := json.Marshal(map[string]string{"label": rest[0]})
		status, raw, fail := accountLiveAccess("POST", "/api/native-main-profiles/register", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			return nativeMainAPIError(raw, stderr, "failed to register the current native login")
		}
		if jsonOut {
			_, _ = stdout.Write(raw)
			fmt.Fprintln(stdout)
			return 0
		}
		fmt.Fprintf(stdout, "Registered '%s' for %s.\n", rest[0], nativeMainHome(raw))
		return 0
	case "add":
		if len(rest) != 1 || jsonOut || yes || rollback {
			fmt.Fprintln(stderr, "benes: usage: account main add <label>")
			return 2
		}
		return runAccountMainAdd(rest[0], stdout, stderr, deps)
	case "switch":
		if len(rest) != 1 || rollback || !yes {
			if !yes {
				fmt.Fprintln(stderr, "Close Codex App/CLI, then pass --yes to confirm it is stopped.")
			}
			fmt.Fprintln(stderr, "benes: usage: account main switch <profile-id-or-label> --yes [--json]")
			return 2
		}
		payload, _ := json.Marshal(map[string]any{"target": rest[0], "confirmedStopped": true})
		status, raw, fail := accountLiveAccess("POST", "/api/native-main-profiles/switch", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			return nativeMainAPIError(raw, stderr, "failed to switch the native login")
		}
		if jsonOut {
			_, _ = stdout.Write(raw)
			fmt.Fprintln(stdout)
			return 0
		}
		fmt.Fprintf(stdout, "Native Codex login for %s is now '%s'. Restart Codex App/CLI before continuing.\n", nativeMainHome(raw), rest[0])
		return 0
	case "recover":
		if len(rest) > 0 || (rollback && !yes) {
			if rollback && !yes {
				fmt.Fprintln(stderr, "--rollback changes the native login and requires --yes.")
			}
			fmt.Fprintln(stderr, "benes: usage: account main recover [--rollback --yes] [--json]")
			return 2
		}
		body := map[string]any{"rollback": rollback}
		if rollback {
			body["confirmedStopped"] = true
		}
		payload, _ := json.Marshal(body)
		status, raw, fail := accountLiveAccess("POST", "/api/native-main-profiles/recover", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			return nativeMainAPIError(raw, stderr, "failed to recover the native-profile transaction")
		}
		if jsonOut {
			_, _ = stdout.Write(raw)
			fmt.Fprintln(stdout)
			return 0
		}
		var result struct {
			Recovered bool   `json:"recovered"`
			Action    string `json:"action"`
		}
		_ = json.Unmarshal(raw, &result)
		if result.Recovered {
			fmt.Fprintf(stdout, "Recovery completed for %s: %s.\n", nativeMainHome(raw), result.Action)
		} else {
			fmt.Fprintf(stdout, "No recovery journal is pending for %s.\n", nativeMainHome(raw))
		}
		return 0
	default:
		fmt.Fprintln(stderr, "benes: usage: account main doctor|list|register|add|switch|recover")
		return 2
	}
}

func nativeMainHome(raw []byte) string {
	var body struct {
		EffectiveCodexHome string `json:"effectiveCodexHome"`
	}
	if json.Unmarshal(raw, &body) == nil && body.EffectiveCodexHome != "" {
		return body.EffectiveCodexHome
	}
	return "the effective CODEX_HOME"
}

func nativeMainAPIError(raw []byte, stderr io.Writer, fallback string) int {
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(raw, &body)
	if body.Error != "" {
		fmt.Fprintf(stderr, "Error: %s\n", body.Error)
		return 1
	}
	fmt.Fprintf(stderr, "Error: %s\n", fallback)
	return 1
}

func runAccountMainAdd(label string, stdout, stderr io.Writer, deps commandDependencies) int {
	status, raw, fail := accountLiveAccess("POST", "/api/native-main-profiles/stage", []byte("{}"), stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		return nativeMainAPIError(raw, stderr, "failed to prepare native login staging")
	}
	var stage struct {
		StageID             string `json:"stageId"`
		WriterToken         string `json:"writerToken"`
		StagingCodexHome    string `json:"stagingCodexHome"`
		EffectiveCodexHome  string `json:"effectiveCodexHome"`
		LeaseExpiresAt      int64  `json:"leaseExpiresAt"`
		HeartbeatIntervalMs int64  `json:"heartbeatIntervalMs"`
	}
	if json.Unmarshal(raw, &stage) != nil || stage.StageID == "" || !filepath.IsAbs(stage.StagingCodexHome) {
		fmt.Fprintln(stderr, "Error: The proxy returned an invalid staging session.")
		return 1
	}
	fmt.Fprintf(stderr, "Effective CODEX_HOME: %s\n", stage.EffectiveCodexHome)
	fmt.Fprintf(stderr, "Starting official Codex login in restricted staging home: %s\n", stage.StagingCodexHome)
	cmd := exec.Command("codex", "login")
	if _, err := exec.LookPath("codex"); err != nil {
		cmd = exec.Command("codex.exe", "login")
	}
	cmd.Env = append(os.Environ(), "CODEX_HOME="+stage.StagingCodexHome)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	ticker := time.NewTicker(time.Duration(max64(stage.HeartbeatIntervalMs, 5000)) * time.Millisecond)
	defer ticker.Stop()
	finished := false
	defer func() {
		if !finished {
			payload, _ := json.Marshal(map[string]string{"stageId": stage.StageID, "writerToken": stage.WriterToken})
			_, _, _ = accountLiveAccess("POST", "/api/native-main-profiles/stage/cancel", payload, stderr, deps)
		}
	}()
	for {
		select {
		case err := <-done:
			if err != nil {
				fmt.Fprintf(stderr, "Error: Official Codex login did not complete successfully.\n")
				return 1
			}
			payload, _ := json.Marshal(map[string]string{"stageId": stage.StageID, "writerToken": stage.WriterToken, "label": label})
			status, raw, fail := accountLiveAccess("POST", "/api/native-main-profiles/stage/finish", payload, stderr, deps)
			if fail != 0 {
				return fail
			}
			if status != 200 {
				return nativeMainAPIError(raw, stderr, "failed to encrypt the staged native login")
			}
			finished = true
			fmt.Fprintf(stdout, "Added encrypted native profile '%s' for %s.\n", label, nativeMainHome(raw))
			return 0
		case <-ticker.C:
			payload, _ := json.Marshal(map[string]string{"stageId": stage.StageID, "writerToken": stage.WriterToken})
			hbStatus, hbRaw, _ := accountLiveAccess("POST", "/api/native-main-profiles/stage/heartbeat", payload, io.Discard, deps)
			if hbStatus == 200 {
				continue
			}
			_ = hbRaw
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			fmt.Fprintln(stderr, "Error: The native-login staging lease was lost before login completed.")
			return 1
		}
	}
}

func max64(v, floor int64) int64 {
	if v < floor {
		return floor
	}
	return v
}
