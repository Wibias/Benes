package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

type apiKeyEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	CreatedAt string `json:"createdAt"`
}

func runAccess(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "key" || args[0] == "keys" {
		if len(args) > 0 && (args[0] == "key" || args[0] == "keys") {
			args = args[1:]
		}
		return runAccessKey(args, stdout, stderr, deps)
	}
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: access key|endpoints|models|test")
		return 2
	}
	switch args[0] {
	case "endpoints":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "benes: usage: access endpoints [--json]")
			return 2
		}
		return runAccessEndpoints(jsonOut, stdout, stderr, deps)
	case "models":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "benes: usage: access models [--json]")
			return 2
		}
		return runAccessModels(jsonOut, stdout, stderr, deps)
	case "test":
		return runAccessTest(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: access key|endpoints|models|test")
		return 2
	}
}

func runAPIKey(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	return runAccessKey(args, stdout, stderr, deps)
}

func runAccessKey(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	args, yes := takeConfigFlag(args, "--yes")
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	switch action {
	case "list":
		if len(args) != 0 {
			fmt.Fprintln(stderr, "benes: usage: access key list [--json]")
			return 2
		}
		return runAccessKeyList(jsonOut, stdout, stderr, deps)
	case "create":
		name := "default"
		if len(args) > 1 {
			fmt.Fprintln(stderr, "benes: usage: access key create [name] [--json]")
			return 2
		}
		if len(args) == 1 {
			name = strings.TrimSpace(args[0])
			if name == "" {
				fmt.Fprintln(stderr, "benes: key name is required")
				return 2
			}
		}
		return runAccessKeyCreate(name, jsonOut, stdout, stderr, deps)
	case "remove", "delete":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "benes: usage: access key remove <id> --yes")
			return 2
		}
		return runAccessKeyRemove(args[0], yes, jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: access key [list|create|remove]")
		return 2
	}
}

func runAccessKeyList(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	keys, err := loadAPIKeys(deps)
	if err != nil {
		return serveFailure(stderr, "load api keys", err)
	}
	type row struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Prefix    string `json:"prefix"`
		CreatedAt string `json:"createdAt"`
	}
	out := make([]row, 0, len(keys))
	for _, key := range keys {
		out = append(out, row{ID: key.ID, Name: key.Name, Prefix: keyPrefix(key.Key), CreatedAt: key.CreatedAt})
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"keys": out})
	}
	if len(out) == 0 {
		fmt.Fprintln(stdout, "No API access keys configured.")
		return 0
	}
	for _, row := range out {
		fmt.Fprintf(stdout, "%s  %s  %s\n", row.ID, row.Name, row.Prefix)
	}
	return 0
}

func runAccessKeyCreate(name string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	entry := apiKeyEntry{
		ID:        newCustomModelID(),
		Name:      name,
		Key:       newDataPlaneKey(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	keys, err := loadAPIKeys(deps)
	if err != nil {
		return serveFailure(stderr, "load api keys", err)
	}
	keys = append(keys, entry)
	if err := saveAPIKeys(stderr, deps, keys); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{
			"id":        entry.ID,
			"name":      entry.Name,
			"key":       entry.Key,
			"createdAt": entry.CreatedAt,
		})
	}
	fmt.Fprintf(stdout, "Created API key %s (%s).\nKey (shown once): %s\n", entry.Name, entry.ID, entry.Key)
	return 0
}

func runAccessKeyRemove(id string, yes, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if !yes {
		fmt.Fprintln(stderr, "benes: remove requires --yes")
		return 1
	}
	keys, err := loadAPIKeys(deps)
	if err != nil {
		return serveFailure(stderr, "load api keys", err)
	}
	var next []apiKeyEntry
	found := false
	for _, key := range keys {
		if key.ID == id {
			found = true
			continue
		}
		next = append(next, key)
	}
	if !found {
		fmt.Fprintf(stderr, "benes: key %q not found\n", id)
		return 1
	}
	if err := saveAPIKeys(stderr, deps, next); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"success": true, "id": id})
	}
	fmt.Fprintf(stdout, "Removed API key %s.\n", id)
	return 0
}

func loadAPIKeys(deps commandDependencies) ([]apiKeyEntry, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	raw, ok := root["apiKeys"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var keys []apiKeyEntry
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func saveAPIKeys(stderr io.Writer, deps commandDependencies, keys []apiKeyEntry) error {
	if len(keys) == 0 {
		return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath("apiKeys"))
		})
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("apiKeys"), encoded)
	})
}

const apiKeyPrefixKeep = 12 // "benes_" + 6 identifying hex

func keyPrefix(key string) string {
	display := strings.Replace(key, "benes_data_", "benes_", 1)
	if len(display) <= apiKeyPrefixKeep {
		return display
	}
	return display[:apiKeyPrefixKeep] + "..."
}

func newDataPlaneKey() string {
	var b [20]byte
	_, _ = rand.Read(b[:])
	return "benes_" + hex.EncodeToString(b[:])
}
