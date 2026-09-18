package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

const (
	defaultReadyWaitSeconds = 45
	maxReadyWaitSeconds     = 300
	readyPollInterval       = 500 * time.Millisecond
)

type readyArgs struct {
	json           bool
	wait           bool
	timeoutSeconds int
}

type readyProbe struct {
	Ready  bool
	Status string
	PID    int
	Port   int
}

func parseReadyArgs(argv []string) (readyArgs, bool) {
	args := readyArgs{timeoutSeconds: defaultReadyWaitSeconds}
	timeoutSet := false
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--json":
			args.json = true
		case "--wait":
			args.wait = true
		case "--timeout":
			if i+1 >= len(argv) {
				return readyArgs{}, false
			}
			raw := argv[i+1]
			if raw == "" {
				return readyArgs{}, false
			}
			for _, c := range raw {
				if c < '0' || c > '9' {
					return readyArgs{}, false
				}
			}
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > maxReadyWaitSeconds {
				return readyArgs{}, false
			}
			args.timeoutSeconds = n
			timeoutSet = true
			i++
		default:
			return readyArgs{}, false
		}
	}
	if timeoutSet && !args.wait {
		return readyArgs{}, false
	}
	return args, true
}

func runReady(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	parsed, ok := parseReadyArgs(args)
	if !ok {
		fmt.Fprintln(stderr, "benes: usage: ready [--json] [--wait [--timeout <seconds>]]")
		return 64
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	probe := deps.probeReady
	if probe == nil {
		probe = defaultProbeReady
	}
	now := deps.now
	if now == nil {
		now = time.Now
	}
	sleep := deps.sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	if !parsed.wait {
		status, pid, port := probeOnce(paths.RuntimePort, probe)
		reportReady(stdout, parsed, status == "ready", status, pid, port)
		if status == "ready" {
			return 0
		}
		return 1
	}

	deadline := now().Add(time.Duration(parsed.timeoutSeconds) * time.Second)
	lastStatus := "unreachable"
	lastPID := 0
	lastPort := 0
	for {
		if !now().Before(deadline) {
			reportReady(stdout, parsed, false, lastStatus, lastPID, lastPort)
			return 1
		}
		status, pid, port := probeOnce(paths.RuntimePort, probe)
		if status == "failed" {
			reportReady(stdout, parsed, false, "failed", pid, port)
			return 1
		}
		if status == "ready" {
			reportReady(stdout, parsed, true, "ready", pid, port)
			return 0
		}
		lastStatus = status
		lastPID = pid
		lastPort = port
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			reportReady(stdout, parsed, false, lastStatus, lastPID, lastPort)
			return 1
		}
		wait := readyPollInterval
		if wait > remaining {
			wait = remaining
		}
		sleep(wait)
	}
}

func probeOnce(runtimePort string, probe func(host string, port int) readyProbe) (status string, pid, port int) {
	state, err := readRuntimePortState(runtimePort)
	if err != nil || state.Port <= 0 {
		return "unreachable", 0, 0
	}
	host := strings.TrimSpace(state.Hostname)
	if host == "" {
		host = "127.0.0.1"
	}
	got := probe(host, state.Port)
	if got.PID == 0 {
		got.PID = state.PID
	}
	if got.Port == 0 {
		got.Port = state.Port
	}
	if got.Status == "" {
		if got.Ready {
			got.Status = "ready"
		} else {
			got.Status = "unreachable"
		}
	}
	return got.Status, got.PID, got.Port
}

func reportReady(stdout io.Writer, args readyArgs, ready bool, status string, pid, port int) {
	if args.json {
		payload := map[string]any{"ready": ready, "status": status, "pid": nil, "port": nil}
		if pid > 0 {
			payload["pid"] = pid
		}
		if port > 0 {
			payload["port"] = port
		}
		_ = json.NewEncoder(stdout).Encode(payload)
		return
	}
	switch status {
	case "ready":
		pidText := "?"
		if pid > 0 {
			pidText = strconv.Itoa(pid)
		}
		portText := "?"
		if port > 0 {
			portText = strconv.Itoa(port)
		}
		fmt.Fprintf(stdout, "Proxy ready (PID %s, port %s)\n", pidText, portText)
	case "pending":
		fmt.Fprintln(stdout, "Proxy running but not ready yet (pending).")
	case "failed":
		fmt.Fprintln(stdout, "Proxy running but not ready (sync failed).")
	default:
		fmt.Fprintln(stdout, "Proxy not reachable or readiness unavailable.")
	}
}

func defaultProbeReady(host string, port int) readyProbe {
	client := &http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/readyz", host, port))
	if err != nil {
		return readyProbe{Status: "unreachable"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readyProbe{Status: "pending", Port: port}
	}
	var body struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || !body.OK {
		return readyProbe{Status: "unreachable", Port: port}
	}
	status := strings.TrimSpace(body.Status)
	if status == "" {
		status = "ready"
	}
	return readyProbe{Ready: status == "ready", Status: status, Port: port}
}
