package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/anthropic"
	"github.com/Wibias/Benes/internal/oauth/commandcode"
	"github.com/Wibias/Benes/internal/oauth/cursor"
	"github.com/Wibias/Benes/internal/oauth/githubcopilot"
	"github.com/Wibias/Benes/internal/oauth/kimi"
	"github.com/Wibias/Benes/internal/oauth/loginown"
	"github.com/Wibias/Benes/internal/oauth/nous"
	"github.com/Wibias/Benes/internal/oauth/xai"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/kiro"
)

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: login requires a provider")
		return 2
	}
	provider := strings.TrimSpace(args[0])
	callback := ""
	apiKey := ""
	baseURL := ""
	force := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--callback":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: login --callback requires a URL or code")
				return 2
			}
			i++
			callback = args[i]
		case "--api-key":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: login --api-key requires a value")
				return 2
			}
			i++
			apiKey = args[i]
		case "--base-url":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: login --base-url requires a value")
				return 2
			}
			i++
			baseURL = args[i]
		case "--force":
			force = true
		default:
			fmt.Fprintf(stderr, "benes: unknown login argument %q\n", args[i])
			return 2
		}
	}
	if provider == "kiro" {
		return runKiroLogin(ctx, force, stdout, stderr, deps)
	}
	if provider == "google-antigravity" {
		return runAntigravityLogin(ctx, callback, stdout, stderr, deps)
	}
	if provider == cursor.ProviderID {
		return runCursorLogin(ctx, stdout, stderr, deps)
	}
	if provider == kimi.ProviderID || provider == nous.ProviderID || provider == githubcopilot.ProviderID {
		return runDeviceOAuthLogin(ctx, provider, stdout, stderr, deps)
	}
	if provider == xai.ProviderID || provider == anthropic.ProviderID || provider == commandcode.ProviderID {
		return runCallbackOAuthLogin(ctx, provider, callback, stdout, stderr, deps)
	}
	return runCatalogLogin(provider, apiKey, baseURL, stdout, stderr, deps)
}

func runAntigravityLogin(ctx context.Context, callback string, stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	pending := antigravity.PendingFile{Path: filepath.Join(paths.Home, "oauth-pending-google-antigravity.json")}
	if callback == "" {
		login, err := antigravity.NewPendingLogin()
		if err != nil {
			return serveFailure(stderr, "start login", err)
		}
		authURL, err := login.AuthURL()
		if err != nil {
			return serveFailure(stderr, "build auth url", err)
		}
		if err := pending.Save(login); err != nil {
			return serveFailure(stderr, "save pending login", err)
		}
		fmt.Fprintln(stdout, authURL)
		fmt.Fprintln(stdout, "Complete Google login, then run: benes login google-antigravity --callback <redirect-url-or-code>")
		return 0
	}
	completed, err := pending.Complete(ctx, http.DefaultClient, callback)
	if err != nil {
		return serveFailure(stderr, "complete login", err)
	}
	if err := antigravity.AppendAccount(filepath.Join(paths.Home, "auth.json"), completed.Account, completed.Refresh); err != nil {
		return serveFailure(stderr, "persist account", err)
	}
	fmt.Fprintf(stdout, "logged in project %s\n", completed.Account.ProjectID)
	return 0
}

func runCursorLogin(ctx context.Context, stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	pending := cursor.PendingFile{Path: filepath.Join(paths.Home, cursor.PendingFileName)}
	login, err := cursor.NewPendingLogin()
	if err != nil {
		return serveFailure(stderr, "start cursor login", err)
	}
	if err := pending.Save(login); err != nil {
		return serveFailure(stderr, "save cursor login", err)
	}
	fmt.Fprintln(stdout, login.AuthURL())
	fmt.Fprintln(stdout, "Approve the Cursor login in your browser, then return here.")
	completed, err := pending.Complete(ctx)
	if err != nil {
		return serveFailure(stderr, "complete cursor login", err)
	}
	if err := cursor.Persist(filepath.Join(paths.Home, "auth.json"), completed.Account); err != nil {
		return serveFailure(stderr, "persist cursor account", err)
	}
	id := completed.Account.ID
	if completed.Account.Email != "" {
		fmt.Fprintf(stdout, "logged in %s (%s)\n", id, completed.Account.Email)
		return 0
	}
	fmt.Fprintf(stdout, "logged in %s\n", id)
	return 0
}

func runCatalogLogin(name, apiKey, baseURL string, stdout, stderr io.Writer, deps commandDependencies) int {
	providers, err := loadInitProviders()
	if err != nil {
		fmt.Fprintf(stderr, "benes: load login providers: %v\n", err)
		return 1
	}
	var selected *initProvider
	for i := range providers {
		if providers[i].ID == name {
			selected = &providers[i]
			break
		}
	}
	if selected == nil {
		fmt.Fprintf(stderr, "benes: unsupported login provider %q\n", name)
		return 2
	}
	switch selected.Kind {
	case "forward":
		fmt.Fprintf(stdout, "%s uses ChatGPT login (forward auth); no API key login.\n", selected.Label)
		return 0
	case "oauth":
		fmt.Fprintf(stderr, "benes: unsupported login provider %q\n", name)
		return 2
	}
	if apiKey == "" && selected.Kind == "key" {
		fmt.Fprintln(stderr, "benes: login --api-key is required for API-key providers")
		return 2
	}
	seed, err := initSeed(*selected, apiKey, baseURL)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	seed, err = preserveModelCosts(deps, name, seed)
	if err != nil {
		return serveFailure(stderr, "merge provider", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if err := tx.Set(config.JSONPath("providers", name), seed); err != nil {
			return err
		}
		root, loadErr := loadConfigRawObject(deps)
		if loadErr != nil {
			return loadErr
		}
		if _, ok := root["defaultProvider"]; !ok {
			encoded, encErr := json.Marshal(name)
			if encErr != nil {
				return encErr
			}
			return tx.Set(config.JSONPath("defaultProvider"), encoded)
		}
		return nil
	}); err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "%s added. Try: benes sync\n", selected.Label)
	return 0
}

func preserveModelCosts(deps commandDependencies, name string, seed json.RawMessage) (json.RawMessage, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return seed, nil
	}
	providersRaw, ok := root["providers"]
	if !ok {
		return seed, nil
	}
	var providers map[string]json.RawMessage
	if json.Unmarshal(providersRaw, &providers) != nil {
		return seed, nil
	}
	existing, ok := providers[name]
	if !ok {
		return seed, nil
	}
	var current map[string]any
	if json.Unmarshal(existing, &current) != nil {
		return seed, nil
	}
	costs, ok := current["modelCosts"]
	if !ok {
		return seed, nil
	}
	var next map[string]any
	if err := json.Unmarshal(seed, &next); err != nil {
		return nil, err
	}
	next["modelCosts"] = costs
	return json.Marshal(next)
}

func runKiroLogin(ctx context.Context, force bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if err := loginown.WaitNotSettling(ctx, "kiro"); err != nil {
		return serveFailure(stderr, "login kiro", err)
	}
	token, err := loginown.TryClaim("kiro")
	if err != nil {
		return serveFailure(stderr, "login kiro", err)
	}
	loginown.BeginSettle("kiro")
	defer loginown.EndSettle("kiro")
	defer loginown.Release(token)
	host := kiro.LiveHost()
	if deps.kiroHost != nil {
		host = deps.kiroHost()
	}
	runner := deps.kiroRunner
	if runner == nil {
		runner = kiro.DefaultCLIRunner(host)
	}
	var pending kiro.PendingLogin
	if force {
		pending, err = kiro.ForceLogin(ctx, host, runner)
	} else {
		pending, err = kiro.Login(ctx, host, runner)
	}
	if err != nil {
		return serveFailure(stderr, "login kiro", err)
	}
	if err := loginown.Assert(token); err != nil {
		kiro.Settle(pending, false)
		return serveFailure(stderr, "login kiro", err)
	}
	if _, err := kiro.ImportSnapshot(pending.Import.Credential); err != nil {
		kiro.Settle(pending, false)
		return serveFailure(stderr, "login kiro", err)
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		kiro.Settle(pending, false)
		return serveFailure(stderr, "login kiro", err)
	}
	if err := kiro.Persist(filepath.Join(paths.Home, "auth.json"), pending.Import.Credential); err != nil {
		kiro.Settle(pending, false)
		return serveFailure(stderr, "persist kiro account", err)
	}
	if err := seedKiroProvider(stderr, deps); err != nil {
		kiro.Settle(pending, false)
		return 1
	}
	kiro.Settle(pending, true)
	fmt.Fprintf(stdout, "logged in kiro profile %s region %s\n", pending.Import.Credential.ProfileARN, pending.Import.Credential.APIRegion)
	return 0
}

func seedKiroProvider(stderr io.Writer, deps commandDependencies) error {
	root, err := loadConfigRawObject(deps)
	if err == nil {
		var providers map[string]json.RawMessage
		if raw, ok := root["providers"]; ok {
			_ = json.Unmarshal(raw, &providers)
		}
		if _, exists := providers["kiro"]; exists {
			return nil
		}
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", "kiro"), kiro.SeedProviderJSON())
	})
}
