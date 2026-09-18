package harnessboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const settingsName = "harnesses.json"
const maxSettingsBytes = 1 << 20

type Settings struct {
	AutoDetect     bool                     `json:"autoDetect"`
	AutoApply      bool                     `json:"autoApply"`
	RetainSnapshot bool                     `json:"retainSnapshot"`
	AllowRestart   bool                     `json:"allowRestart"`
	Sidecars       *harnesspolicy.Overrides `json:"sidecars,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		AutoDetect:     true,
		AutoApply:      true,
		RetainSnapshot: true,
		AllowRestart:   false,
	}
}

type fileDoc struct {
	Clients map[string]Settings `json:"clients"`
}

func SettingsPath(benesHome string) string {
	return filepath.Join(benesHome, settingsName)
}

func LoadSettings(benesHome string) (map[string]Settings, error) {
	out := map[string]Settings{}
	path := SettingsPath(benesHome)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if len(raw) > maxSettingsBytes {
		return nil, fmt.Errorf("harness settings file is too large")
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("harness settings file is not valid JSON")
	}
	for id, row := range doc.Clients {
		if !Known(id) {
			continue
		}
		if err := validateSidecars(row.Sidecars); err != nil {
			return nil, fmt.Errorf("harness settings for %q are invalid: %w", id, err)
		}
		row.Sidecars = normalizeSidecars(row.Sidecars)
		out[id] = row
	}
	return out, nil
}

func SaveSettings(benesHome string, all map[string]Settings) error {
	if err := os.MkdirAll(benesHome, 0o700); err != nil {
		return err
	}
	doc := fileDoc{Clients: map[string]Settings{}}
	for id, row := range all {
		if !Known(id) {
			continue
		}
		if err := validateSidecars(row.Sidecars); err != nil {
			return fmt.Errorf("harness settings for %q are invalid: %w", id, err)
		}
		row.Sidecars = normalizeSidecars(row.Sidecars)
		doc.Clients[id] = row
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicfile.Write(SettingsPath(benesHome), raw, atomicfile.Options{Mode: 0o600})
}

func PutSettings(benesHome, id string, row Settings) (Settings, error) {
	if !Known(id) {
		return Settings{}, fmt.Errorf("unknown harness %q", id)
	}
	if err := validateSidecars(row.Sidecars); err != nil {
		return Settings{}, err
	}
	row.Sidecars = normalizeSidecars(row.Sidecars)
	all, err := LoadSettings(benesHome)
	if err != nil {
		return Settings{}, err
	}
	if all == nil {
		all = map[string]Settings{}
	}
	all[id] = row
	if err := SaveSettings(benesHome, all); err != nil {
		return Settings{}, err
	}
	return row, nil
}

func validateSidecars(overrides *harnesspolicy.Overrides) error {
	if overrides == nil {
		return nil
	}
	return overrides.Validate()
}

func normalizeSidecars(overrides *harnesspolicy.Overrides) *harnesspolicy.Overrides {
	if overrides == nil || (overrides.WebSearch == nil && overrides.Vision == nil) {
		return nil
	}
	return overrides
}

func settingsFor(all map[string]Settings, id string) Settings {
	if row, ok := all[id]; ok {
		return row
	}
	return DefaultSettings()
}
