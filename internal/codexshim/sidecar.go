package codexshim

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Sidecar struct {
	Marker string `json:"marker"`
	Benes  string `json:"benes,omitempty"`
	Backup string `json:"backup"`
}

func SidecarPath(wrapper string) string {
	return wrapper + ".benes-shim.json"
}

func WriteSidecar(path string, spec Sidecar) error {
	if strings.TrimSpace(spec.Marker) == "" {
		spec.Marker = Marker
	}
	if strings.TrimSpace(spec.Backup) == "" {
		return fmt.Errorf("sidecar backup path is required")
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o600)
}

func ReadSidecar(path string) (*Sidecar, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) > 1<<16 {
		return nil, fmt.Errorf("codex-shim sidecar is too large")
	}
	var spec Sidecar
	if json.Unmarshal(raw, &spec) != nil || spec.Marker != Marker || strings.TrimSpace(spec.Backup) == "" {
		return nil, fmt.Errorf("codex-shim sidecar is invalid")
	}
	return &spec, nil
}
