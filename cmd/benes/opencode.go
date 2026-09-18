package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/export"
)

const opencodeInstallHint = "`opencode` CLI not found. Install it first: npm install -g opencode-ai"

var launchOpencode = defaultLaunchOpencode

func runOpencode(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	_ = stdout
	if err := ensureLiveProxy(stderr, deps); err != nil {
		return 1
	}
	code, payload, err := observeGET("/api/client-config?client=opencode", stderr, deps)
	if err != nil {
		return 1
	}
	if code != 0 {
		return code
	}
	var body struct {
		Config     any    `json:"config"`
		ModelCount int    `json:"modelCount"`
		Text       string `json:"text"`
	}
	if json.Unmarshal(payload, &body) != nil {
		fmt.Fprintln(stderr, "benes: unexpected export payload")
		return 1
	}
	if strings.Contains(body.Text, "sk-") {
		fmt.Fprintln(stderr, "benes: opencode refused to inject a secret into runtime config")
		return 1
	}
	block, err := export.OpencodeProviderBlock(body.Config)
	if err != nil {
		fmt.Fprintf(stderr, "benes: opencode: %v\n", err)
		return 1
	}
	merged, err := export.MergeOpencodeRuntime(os.Getenv(export.ConfigContentEnv), block)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 1
	}
	content, err := json.Marshal(merged)
	if err != nil {
		return serveFailure(stderr, "encode opencode runtime config", err)
	}
	if strings.Contains(string(content), "sk-") {
		fmt.Fprintln(stderr, "benes: opencode refused to inject a secret into runtime config")
		return 1
	}
	apiKey := opencodeAdmissionKey()
	fmt.Fprintf(stderr, "opencode wired to the running proxy — %d model(s) under provider `%s`.\n", body.ModelCount, export.ProviderID)
	fmt.Fprintln(stderr, "Existing opencode config files are left untouched; only the runtime provider block is injected.")
	env := append([]string{}, os.Environ()...)
	env = setEnvValue(env, export.ConfigContentEnv, string(content))
	env = setEnvValue(env, export.OpenCodeAPIKeyEnv, apiKey)
	if err := launchOpencode(args, env); err != nil {
		if isOpencodeMissing(err) {
			fmt.Fprintln(stderr, opencodeInstallHint)
			return 1
		}
		fmt.Fprintf(stderr, "benes: failed to launch opencode: %v\n", err)
		return 1
	}
	return 0
}

func ensureLiveProxy(stderr io.Writer, deps commandDependencies) error {
	if _, err := liveProxyBase(deps); err == nil {
		return nil
	}
	spawn := deps.spawnStart
	if spawn == nil {
		spawn = defaultSpawnStart
	}
	if err := spawn(); err != nil {
		fmt.Fprintf(stderr, "benes: start proxy: %v\n", err)
		return err
	}
	sleep := deps.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for i := 0; i < 40; i++ {
		if _, err := liveProxyBase(deps); err == nil {
			return nil
		}
		sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(stderr, "benes: proxy did not become healthy after starting.")
	return fmt.Errorf("proxy not running")
}

func opencodeAdmissionKey() string {
	if token := strings.TrimSpace(os.Getenv("BENES_API_AUTH_TOKEN")); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("BENES_API_AUTH_TOKEN")); token != "" {
		return token
	}
	return "benes"
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return append(out, prefix+value)
}

func defaultLaunchOpencode(args []string, env []string) error {
	name, err := exec.LookPath("opencode")
	if err != nil && runtime.GOOS == "windows" {
		name, err = exec.LookPath("opencode.cmd")
	}
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}

func isOpencodeMissing(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && runtime.GOOS == "windows" && exit.ExitCode() == 9009 {
		return true
	}
	return false
}
