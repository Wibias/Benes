package wintray

import (
	"embed"
	"io/fs"
)

//go:embed windows-tray.ps1 assets/*.ico
var embedded embed.FS

func EmbeddedScript() ([]byte, error) {
	return embedded.ReadFile("windows-tray.ps1")
}

func EmbeddedIcons() (map[string][]byte, error) {
	entries, err := fs.ReadDir(embedded, "assets")
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := embedded.ReadFile("assets/" + entry.Name())
		if err != nil {
			return nil, err
		}
		out[entry.Name()] = body
	}
	return out, nil
}
