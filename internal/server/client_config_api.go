package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/export"
)

func (h *handler) serveClientConfigAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/client-config" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	if client == "" {
		writeError(w, http.StatusBadRequest, "invalid_client", "client must be one of: "+strings.Join(export.ClientIDs, ", "))
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	host, portStr, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = "127.0.0.1"
		portStr = "23100"
	}
	port, conv := strconv.Atoi(portStr)
	if conv != nil || port <= 0 {
		port = 23100
	}
	result, err := export.Build(client, export.Context{
		BaseURL:  "http://" + host + ":" + strconv.Itoa(port) + "/v1",
		Hostname: host,
		Models:   h.exportModels(),
		Direct:   h.openaiDirect(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "export_failed", err.Error())
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client":              result.Client,
		"filename":            result.Filename,
		"destination":         result.Destination,
		"apiKeyEnv":           result.APIKeyEnv,
		"exportHint":          result.ExportHint,
		"format":              result.Format,
		"mediaType":           result.MediaType,
		"text":                result.Text,
		"modelCount":          result.ModelCount,
		"modelsWithoutLimits": result.ModelsWithoutLimits,
		"config":              result.Document,
	})
	return true
}

func (h *handler) exportModels() []export.Model {
	disabled := map[string]struct{}{}
	for _, id := range h.loadDisabledModels() {
		disabled[id] = struct{}{}
	}
	out := make([]export.Model, 0, len(h.catalogModels))
	for _, model := range h.catalogModels {
		if model.ID == "" {
			continue
		}
		provider, id := splitNamespaced(model.ID)
		if _, hide := disabled[model.ID]; hide {
			continue
		}
		if _, hide := disabled[id]; hide {
			continue
		}
		item := export.Model{
			Namespaced:       model.ID,
			Provider:         provider,
			ID:               id,
			ContextWindow:    model.Context.Tokens,
			ReasoningEfforts: append([]string(nil), model.ReasoningEfforts...),
		}
		if provider == "openai" {
			item.Native = true
		}
		if model.Vision == catalog.CapabilityTrue {
			item.InputModalities = []string{"text", "image"}
		}
		out = append(out, item)
	}
	return out
}

func (h *handler) openaiDirect() bool {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return false
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return false
	}
	raw, ok := disk.Providers["openai"]
	if !ok {
		return false
	}
	var rec struct {
		CodexAccountMode string `json:"codexAccountMode"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return false
	}
	return rec.CodexAccountMode == "direct"
}
