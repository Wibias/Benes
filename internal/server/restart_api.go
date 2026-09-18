package server

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/codexcache"
	"github.com/Wibias/Benes/internal/codexcatalog"
	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveCodexAppServerAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/system/codex-app-server" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, codexappserver.ReadState(h.codexAppServerService(r)))
	return true
}

func (h *handler) serveCodexRestartAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/system/codex-restart" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	writeJSON(w, http.StatusOK, codexappserver.PerformRestart(h.codexAppServerService(r)))
	return true
}

func (h *handler) codexAppServerService(r *http.Request) codexappserver.ServiceIO {
	io := h.codexAppServer
	if strings.TrimSpace(io.Process.CatalogHome) == "" {
		io.Process.CatalogHome = strings.TrimSpace(h.codexHome)
	}
	if io.SyncCatalog == nil {
		io.SyncCatalog = func(port int) (bool, error) {
			return h.syncCodexCatalog(r, port)
		}
	}
	if io.ListenPort == nil {
		io.ListenPort = func() int { return loopbackListenPort(r) }
	}
	return io
}

func loopbackListenPort(r *http.Request) int {
	_, portStr, err := net.SplitHostPort(r.Host)
	if err != nil {
		return 23100
	}
	port, conv := strconv.Atoi(portStr)
	if conv != nil || port <= 0 {
		return 23100
	}
	return port
}

func (h *handler) syncCodexCatalog(r *http.Request, port int) (bool, error) {
	home := strings.TrimSpace(h.codexHome)
	if home == "" {
		resolved, err := config.ResolveCodexHome(config.CodexHomeOptions{})
		if err != nil {
			return false, err
		}
		home = resolved
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = "127.0.0.1"
	}
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = loopbackListenPort(r)
	}
	inject, err := codexrestore.Inject(home, fmt.Sprintf("http://%s:%d/v1", host, port))
	if err != nil {
		return false, err
	}
	catalogWrote, err := codexcatalog.WriteWithAliases(home, h.exposedCatalogModels(), h.aliases)
	if err != nil {
		return false, err
	}
	cacheWrote, err := codexcache.Invalidate(home)
	if err != nil {
		return false, err
	}
	return inject.Changed || catalogWrote || cacheWrote, nil
}
