package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var usableGatewayID = regexp.MustCompile(`(?i)^(claude|anthropic)`)

type GatewayModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

func WriteGatewayModelCache(baseURL string, models []GatewayModel, configDir string) (string, error) {
	usable := make([]GatewayModel, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" || !usableGatewayID.MatchString(id) {
			continue
		}
		row := GatewayModel{ID: id}
		if strings.TrimSpace(model.DisplayName) != "" {
			row.DisplayName = model.DisplayName
		}
		usable = append(usable, row)
	}
	payload, err := json.Marshal(map[string]any{
		"baseUrl":   baseURL,
		"fetchedAt": time.Now().UnixMilli(),
		"models":    usable,
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(configDir, "cache", "gateway-models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := atomicfile.Write(path, payload, atomicfile.Options{Mode: 0o600}); err != nil {
		return "", err
	}
	return path, nil
}
