package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
)

type customModel struct {
	ID               string   `json:"id"`
	Provider         string   `json:"provider"`
	ModelID          string   `json:"modelId"`
	DisplayName      string   `json:"displayName,omitempty"`
	ContextWindow    int      `json:"contextWindow,omitempty"`
	InputModalities  []string `json:"inputModalities,omitempty"`
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
	DefaultReasoning string   `json:"defaultReasoningEffort,omitempty"`
	AddedAt          string   `json:"addedAt"`
}

func runModels(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	args, yes := takeConfigFlag(args, "--yes")
	if len(args) == 0 || args[0] == "list" {
		if len(args) > 1 {
			fmt.Fprintln(stderr, "benes: usage: models [list] [--json]")
			return 2
		}
		return runModelsList(jsonOut, stdout, stderr, deps)
	}
	switch args[0] {
	case "add":
		return runModelsAdd(args[1:], stdout, stderr, deps)
	case "remove":
		return runModelsRemove(args[1:], yes, stdout, stderr, deps)
	case "list-custom":
		return runModelsListCustom(jsonOut, stdout, stderr, deps)
	case "live":
		return runModelsList(jsonOut, stdout, stderr, deps)
	case "edit":
		return runModelsEdit(args[1:], jsonOut, stdout, stderr, deps)
	case "enable":
		return runModelsEnable(args[1:], true, jsonOut, stdout, stderr, deps)
	case "disable":
		return runModelsEnable(args[1:], false, jsonOut, stdout, stderr, deps)
	case "provider":
		return runModelsProviderVisibility(args[1:], jsonOut, stdout, stderr, deps)
	case "selected":
		return runModelsSelected(args[1:], jsonOut, stdout, stderr, deps)
	case "context":
		return runModelsContext(args[1:], jsonOut, stdout, stderr, deps)
	case "shadow":
		return runModelsShadow(args[1:], jsonOut, stdout, stderr, deps)
	case "probe":
		return runModelsProbe(args[1:], jsonOut, stdout, stderr, deps)
	case "preset":
		return runModelsPreset(args[1:], jsonOut, stdout, stderr, deps)
	case "new-policy":
		return runModelsNewPolicy(args[1:], jsonOut, stdout, stderr, deps)
	case "new-arrivals":
		return runModelsNewArrivals(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: models [list|live|add|edit|remove|list-custom|enable|disable|provider|selected|context|shadow|probe|preset|new-policy|new-arrivals]")
		return 2
	}
}

func runModelsList(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	models := bootstrap.ListCatalogModels(disk)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	if jsonOut {
		if err := json.NewEncoder(stdout).Encode(ids); err != nil {
			return serveFailure(stderr, "encode models", err)
		}
		return 0
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "no catalog models")
		return 0
	}
	for _, id := range ids {
		fmt.Fprintln(stdout, id)
	}
	return 0
}

func runModelsAdd(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "benes: usage: models add <provider> <modelId> [--display-name <name>] [--context-window <tokens>] [--modalities text,image,audio]")
		return 2
	}
	provider := strings.TrimSpace(args[0])
	modelID := strings.TrimSpace(args[1])
	rest := append([]string{}, args[2:]...)
	displayName, rest := takeOption(rest, "--display-name")
	contextRaw, rest := takeOption(rest, "--context-window")
	modalitiesRaw, rest := takeOption(rest, "--modalities")
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(rest, " "))
		return 2
	}
	if provider == "" || modelID == "" || strings.Contains(provider, "/") {
		fmt.Fprintln(stderr, "benes: provider and modelId are required")
		return 2
	}
	if strings.Contains(displayName, "/") {
		fmt.Fprintln(stderr, "benes: displayName must not contain /")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	if _, ok := disk.Providers[provider]; !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", provider)
		return 1
	}
	existing, err := loadCustomModels(deps)
	if err != nil {
		return serveFailure(stderr, "load custom models", err)
	}
	slug := provider + "/" + modelID
	for _, model := range existing {
		if model.Provider+"/"+model.ModelID == slug {
			fmt.Fprintf(stderr, "benes: custom model %q already exists\n", slug)
			return 1
		}
	}
	entry := customModel{
		ID:       newCustomModelID(),
		Provider: provider,
		ModelID:  modelID,
		AddedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if displayName != "" {
		entry.DisplayName = displayName
	}
	if contextRaw != "" {
		var tokens int
		if _, err := fmt.Sscanf(contextRaw, "%d", &tokens); err != nil || tokens <= 0 {
			fmt.Fprintln(stderr, "benes: context window must be a positive integer")
			return 2
		}
		entry.ContextWindow = tokens
	}
	if modalitiesRaw != "" {
		mods := strings.Split(modalitiesRaw, ",")
		allowed := map[string]bool{"text": true, "image": true, "audio": true}
		seen := map[string]bool{}
		var out []string
		for _, mod := range mods {
			mod = strings.TrimSpace(mod)
			if !allowed[mod] || seen[mod] {
				if !allowed[mod] {
					fmt.Fprintln(stderr, "benes: modalities must be comma-separated values from text|image|audio")
					return 2
				}
				continue
			}
			seen[mod] = true
			out = append(out, mod)
		}
		if len(out) == 0 {
			fmt.Fprintln(stderr, "benes: modalities must be comma-separated values from text|image|audio")
			return 2
		}
		entry.InputModalities = out
	}
	existing = append(existing, entry)
	if err := saveCustomModels(stderr, deps, existing); err != nil {
		return 1
	}
	if err := retainCustomModelOnPreset(stderr, deps, provider, modelID); err != nil {
		return serveFailure(stderr, "keep custom model on preset", err)
	}
	fmt.Fprintf(stdout, "Added custom model %s (%s).\n", slug, entry.ID)
	return 0
}

func runModelsRemove(args []string, yes bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: models remove <customId|provider/modelId> [--yes]")
		return 2
	}
	if !yes {
		fmt.Fprintln(stderr, "benes: remove requires --yes in non-interactive mode")
		return 1
	}
	target := strings.TrimSpace(args[0])
	existing, err := loadCustomModels(deps)
	if err != nil {
		return serveFailure(stderr, "load custom models", err)
	}
	matches := []int{}
	for i, model := range existing {
		if target == model.ID || target == model.Provider+"/"+model.ModelID {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		fmt.Fprintf(stderr, "benes: custom model %q not found\n", target)
		return 1
	}
	if len(matches) > 1 {
		fmt.Fprintf(stderr, "benes: custom model selector %q is ambiguous; use the custom model id\n", target)
		return 1
	}
	index := matches[0]
	next := append(append([]customModel{}, existing[:index]...), existing[index+1:]...)
	if err := saveCustomModels(stderr, deps, next); err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "Removed custom model %s/%s.\n", existing[index].Provider, existing[index].ModelID)
	return 0
}

func runModelsListCustom(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	existing, err := loadCustomModels(deps)
	if err != nil {
		return serveFailure(stderr, "load custom models", err)
	}
	if jsonOut {
		if err := json.NewEncoder(stdout).Encode(existing); err != nil {
			return serveFailure(stderr, "encode custom models", err)
		}
		return 0
	}
	if len(existing) == 0 {
		fmt.Fprintln(stdout, "no custom models")
		return 0
	}
	for _, model := range existing {
		fmt.Fprintf(stdout, "%s/%s  %s\n", model.Provider, model.ModelID, model.ID)
	}
	return 0
}

func loadCustomModels(deps commandDependencies) ([]customModel, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	raw, ok := root["customModels"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var models []customModel
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, err
	}
	return models, nil
}

func saveCustomModels(stderr io.Writer, deps commandDependencies, models []customModel) error {
	if len(models) == 0 {
		return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath("customModels"))
		})
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("customModels"), encoded)
	})
}

func takeOption(args []string, flag string) (string, []string) {
	for i := 0; i < len(args); i++ {
		if args[i] != flag {
			continue
		}
		if i+1 >= len(args) {
			return "", args
		}
		value := args[i+1]
		return value, append(append([]string{}, args[:i]...), args[i+2:]...)
	}
	return "", args
}

func newCustomModelID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	text := hex.EncodeToString(b[:])
	return text[0:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}
