package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

func runLogout(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(stderr, "benes: usage: logout <provider|credential-id>")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	store, err := credentials.NewFileStore(filepath.Join(paths.Home, "credentials"), true)
	if err != nil {
		fmt.Fprintf(stderr, "benes: open credential store: %v\n", err)
		return 1
	}
	name := strings.TrimSpace(args[0])
	id := logoutCredentialID(paths.Config, name)
	if err := store.Delete(credentials.Ref{ID: id, Source: credentials.SourceSecureStore}); err != nil {
		fmt.Fprintf(stderr, "benes: logout %s: %v\n", name, err)
		return 1
	}
	if id != name {
		_ = store.Delete(credentials.Ref{ID: name, Source: credentials.SourceSecureStore})
	}
	fmt.Fprintf(stdout, "logged out %s\n", name)
	return 0
}

func logoutCredentialID(configPath, name string) string {
	disk, err := config.LoadDiskConfig(configPath, 0)
	if err != nil {
		return name
	}
	raw, ok := disk.Providers[name]
	if !ok {
		return name
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return name
	}
	refRaw, ok := obj["credentialRef"]
	if !ok {
		return name
	}
	var ref credentials.Ref
	if json.Unmarshal(refRaw, &ref) != nil || strings.TrimSpace(ref.ID) == "" {
		return name
	}
	return strings.TrimSpace(ref.ID)
}
