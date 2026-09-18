package codexshim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Marker = "benes codex autostart shim"
const StateFile = "codex-shim.json"

type File struct {
	WrapperPath  string `json:"wrapperPath"`
	OriginalPath string `json:"originalPath"`
	BackupPath   string `json:"backupPath"`
	PreserveOnly bool   `json:"preserveOnly"`
}

type State struct {
	Platform string `json:"platform"`
	Wrappers []File `json:"wrappers"`
	File
}

func StatePath(home string) string {
	return filepath.Join(home, StateFile)
}

func Status(home string) string {
	state, err := ReadState(home)
	if err != nil {
		return "Codex autostart shim state is invalid or corrupt."
	}
	if state == nil {
		return "Codex autostart shim is not installed."
	}
	files := state.Files()
	if len(files) == 0 {
		return "Codex autostart shim state is invalid or corrupt."
	}
	return fmt.Sprintf("Codex autostart shim installed (%d wrapper(s)).", len(files))
}

func Uninstall(home string) (bool, string, error) {
	state, err := ReadState(home)
	if err != nil {
		return false, "", err
	}
	if state == nil {
		return false, "Codex autostart shim is not installed.", nil
	}
	files := state.Files()
	for _, file := range files {
		if file.PreserveOnly {
			continue
		}
		if isShim(file.WrapperPath) {
			_ = os.Remove(file.WrapperPath)
		}
		_ = os.Remove(SidecarPath(file.WrapperPath))
	}
	for _, file := range files {
		if _, err := os.Stat(file.BackupPath); err != nil {
			continue
		}
		if _, err := os.Stat(file.OriginalPath); err == nil {
			continue
		}
		if err := os.Rename(file.BackupPath, file.OriginalPath); err != nil {
			return false, "", err
		}
	}
	if err := os.Remove(StatePath(home)); err != nil && !os.IsNotExist(err) {
		return false, "", err
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.OriginalPath)
	}
	return true, "Codex autostart shim removed. Restored " + strings.Join(paths, ", ") + ".", nil
}

func ReadState(home string) (*State, error) {
	raw, err := os.ReadFile(StatePath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("codex-shim state is too large")
	}
	var state State
	if json.Unmarshal(raw, &state) != nil || strings.TrimSpace(state.Platform) == "" {
		return nil, fmt.Errorf("codex-shim state is invalid")
	}
	if len(state.Files()) == 0 {
		return nil, fmt.Errorf("codex-shim state is invalid")
	}
	return &state, nil
}

func (s State) Files() []File {
	if len(s.Wrappers) > 0 {
		out := make([]File, 0, len(s.Wrappers))
		for _, file := range s.Wrappers {
			if file.valid() {
				out = append(out, file)
			}
		}
		return out
	}
	if s.File.valid() {
		return []File{s.File}
	}
	return nil
}

func (f File) valid() bool {
	return strings.TrimSpace(f.WrapperPath) != "" && strings.TrimSpace(f.OriginalPath) != "" && strings.TrimSpace(f.BackupPath) != ""
}

func isShim(path string) bool {
	if spec, err := ReadSidecar(SidecarPath(path)); err == nil && spec != nil {
		return true
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > 1<<20 {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), Marker)
}
