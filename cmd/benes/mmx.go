package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	mmxInstallHint = "`mmx` CLI not found. Install MiniMax CLI first."
	mmxLoopbackKey = "benes-loopback"
)

var (
	mmxBooleanFlags = map[string]struct{}{
		"--quiet": {}, "--verbose": {}, "--no-color": {}, "--dry-run": {},
		"--non-interactive": {}, "--yes": {}, "--async": {}, "--stream": {},
		"--no-stream": {}, "--no-wait": {}, "--help": {}, "--version": {},
	}
	launchMmx = defaultLaunchNamedClient
)

func runMmx(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	_ = stdout
	if mmxStandaloneInfo(args) {
		if err := launchMmx("mmx", args, os.Environ()); err != nil {
			if isOpencodeMissing(err) {
				fmt.Fprintln(stderr, mmxInstallHint)
				return 1
			}
			fmt.Fprintf(stderr, "benes: failed to launch mmx: %v\n", err)
			return 1
		}
		return 0
	}
	if flag := mmxUnsafeOverride(args); flag != "" {
		fmt.Fprintf(stderr, "benes: %s is not accepted by mmx because it could bypass the proxy or expose a caller credential.\n", flag)
		return 2
	}
	if path := mmxCommandPath(args); len(path) == 0 || path[0] != "text" {
		fmt.Fprintln(stderr, "benes: mmx supports only `mmx text` commands. Use plain `mmx` for MiniMax image, video, speech, music, vision, search, quota, auth, config, file, and update APIs.")
		return 2
	}
	if err := ensureLiveProxy(stderr, deps); err != nil {
		return 1
	}
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintln(stderr, "benes: proxy not running. Start it with benes start.")
		return 1
	}
	if !isLoopbackHost(probeHost(hostFromBase(base))) {
		fmt.Fprintln(stderr, "benes: mmx is loopback-only; MMX has no field for a remote-admission header.")
		return 2
	}
	configDir, err := os.MkdirTemp("", "benes-mmx-")
	if err != nil {
		return serveFailure(stderr, "create mmx config dir", err)
	}
	defer os.RemoveAll(configDir)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{\n  \"api_key\": \""+mmxLoopbackKey+"\",\n  \"region\": \"global\"\n}\n"), 0o600); err != nil {
		return serveFailure(stderr, "write mmx config", err)
	}
	upstream := strings.TrimRight(base, "/")
	bridge, err := startMmxTextBridge(upstream)
	if err != nil {
		fmt.Fprintf(stderr, "benes: mmx bridge: %v\n", err)
		return 1
	}
	defer bridge.Close()
	fmt.Fprintf(stderr, "MiniMax CLI text bridged to %s/v1/messages.\n", upstream)
	env := buildMmxEnv(os.Environ(), configDir, "http://127.0.0.1:"+bridge.port)
	if err := launchMmx("mmx", args, env); err != nil {
		if isOpencodeMissing(err) {
			fmt.Fprintln(stderr, mmxInstallHint)
			return 1
		}
		fmt.Fprintf(stderr, "benes: failed to launch mmx: %v\n", err)
		return 1
	}
	return 0
}

func mmxStandaloneInfo(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "--help", "-h", "--version", "-v":
		return true
	default:
		return false
	}
}

func mmxUnsafeOverride(args []string) string {
	for _, arg := range args {
		name := arg
		if i := strings.IndexByte(arg, '='); i >= 0 {
			name = arg[:i]
		}
		switch name {
		case "--api-key", "--base-url", "--region":
			return name
		}
	}
	return ""
}

func mmxCommandPath(args []string) []string {
	path := []string{}
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			name := arg
			if eq := strings.IndexByte(arg, '='); eq >= 0 {
				name = arg[:eq]
			}
			if _, ok := mmxBooleanFlags[name]; ok || eqIndex(arg) >= 0 {
				i++
				continue
			}
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "-") {
			i++
			continue
		}
		path = append(path, arg)
		i++
	}
	return path
}

func eqIndex(arg string) int { return strings.IndexByte(arg, '=') }

func buildMmxEnv(env []string, configDir, baseURL string) []string {
	owned := map[string]struct{}{
		"MMX_CONFIG_DIR": {}, "MINIMAX_BASE_URL": {}, "MINIMAX_REGION": {},
		"MINIMAX_API_KEY": {}, "HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {},
	}
	out := make([]string, 0, len(env)+3)
	for _, item := range env {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		if _, drop := owned[strings.ToUpper(key)]; drop {
			continue
		}
		out = append(out, item)
	}
	return append(out,
		"MMX_CONFIG_DIR="+configDir,
		"MINIMAX_BASE_URL="+baseURL,
		"MINIMAX_REGION=global",
	)
}

type mmxBridge struct {
	server *http.Server
	ln     net.Listener
	port   string
}

func (b *mmxBridge) Close() {
	if b == nil {
		return
	}
	_ = b.server.Close()
	_ = b.ln.Close()
}

func startMmxTextBridge(upstream string) (*mmxBridge, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 0}
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"type":"error","error":{"type":"not_found_error","message":"unsupported MMX bridge route"}}`, http.StatusNotFound)
			return
		}
		canonical := ""
		switch r.URL.Path {
		case "/anthropic/v1/messages":
			canonical = "/v1/messages"
		case "/anthropic/v1/messages/count_tokens":
			canonical = "/v1/messages/count_tokens"
		default:
			http.Error(w, `{"type":"error","error":{"type":"not_found_error","message":"unsupported MMX bridge route"}}`, http.StatusNotFound)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(upstream, "/")+canonical+"?"+r.URL.RawQuery, r.Body)
		if err != nil {
			http.Error(w, `{"type":"error","error":{"type":"api_error","message":"Benes proxy unavailable"}}`, http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		req.Header.Del("Authorization")
		req.Header.Del("X-Benes-Api-Key")
		req.Header.Del("Host")
		req.Header.Del("Content-Length")
		req.Header.Set("X-Api-Key", mmxLoopbackKey)
		res, err := client.Do(req)
		if err != nil {
			http.Error(w, `{"type":"error","error":{"type":"api_error","message":"Benes proxy unavailable"}}`, http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		for key, values := range res.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}
	mux.HandleFunc("/", handler)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(ln) }()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return &mmxBridge{server: server, ln: ln, port: port}, nil
}
