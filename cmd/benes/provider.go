package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/Wibias/Benes/internal/config"
)

type providerRecord struct {
	Adapter             string          `json:"adapter"`
	BaseURL             string          `json:"baseUrl"`
	APIKey              string          `json:"apiKey,omitempty"`
	APIKeyTransport     string          `json:"apiKeyTransport,omitempty"`
	DefaultModel        string          `json:"defaultModel,omitempty"`
	AllowPrivateNetwork bool            `json:"allowPrivateNetwork,omitempty"`
	AuthMode            string          `json:"authMode,omitempty"`
	Note                string          `json:"note,omitempty"`
	Headers             json.RawMessage `json:"headers,omitempty"`
	Disabled            bool            `json:"disabled,omitempty"`
	LiveModels          *bool           `json:"liveModels,omitempty"`
}

func runProvider(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "benes: usage: provider list|add|edit|remove|show|set-default|selected|account-mode|test|presets|quota")
		return 0
	}
	switch args[0] {
	case "list":
		return runProviderList(jsonOut, stdout, stderr, deps)
	case "add":
		return runProviderAdd(args[1:], jsonOut, stdout, stderr, deps)
	case "edit":
		return runProviderEdit(args[1:], jsonOut, stdout, stderr, deps)
	case "remove":
		return runProviderRemove(args[1:], jsonOut, stdout, stderr, deps)
	case "show":
		return runProviderShow(args[1:], jsonOut, stdout, stderr, deps)
	case "set-default":
		return runProviderSetDefault(args[1:], stdout, stderr, deps)
	case "selected":
		return runModelsSelected(args[1:], jsonOut, stdout, stderr, deps)
	case "account-mode":
		return runProviderAccountMode(args[1:], jsonOut, stdout, stderr, deps)
	case "test":
		return runProviderTest(args[1:], jsonOut, stdout, stderr, deps)
	case "presets":
		return runProviderPresets(jsonOut, stdout, stderr, deps)
	case "quota":
		return runProviderQuota(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: provider list|add|edit|remove|show|set-default|selected|account-mode|test|presets|quota")
		return 2
	}
}

func runProviderList(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	providers, defaultName, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	names := providerNames(providers)
	if jsonOut {
		type row struct {
			Name         string `json:"name"`
			Adapter      string `json:"adapter"`
			BaseURL      string `json:"baseUrl"`
			DefaultModel string `json:"defaultModel,omitempty"`
			IsDefault    bool   `json:"isDefault"`
		}
		out := make([]row, 0, len(names))
		for _, name := range names {
			prov := providers[name]
			out = append(out, row{
				Name:         name,
				Adapter:      prov.Adapter,
				BaseURL:      prov.BaseURL,
				DefaultModel: prov.DefaultModel,
				IsDefault:    name == defaultName,
			})
		}
		return encodeJSON(stdout, stderr, map[string]any{"configured": out})
	}
	if len(names) == 0 {
		fmt.Fprintln(stdout, "no configured providers")
		return 0
	}
	fmt.Fprintln(stdout, "Configured providers:")
	for _, name := range names {
		prov := providers[name]
		mark := ""
		if name == defaultName {
			mark = " (default)"
		}
		fmt.Fprintf(stdout, "  %s%s  adapter=%s", name, mark, prov.Adapter)
		if prov.DefaultModel != "" {
			fmt.Fprintf(stdout, " model=%s", prov.DefaultModel)
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func runProviderAdd(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, force := takeConfigFlag(args, "--force")
	args, setDefault := takeConfigFlag(args, "--set-default")
	args, allowPrivate := takeConfigFlag(args, "--allow-private-network")
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "benes: usage: provider add <name> --adapter <adapter> --base-url <url> [--api-key <key>] [--default-model <model>] [--force] [--set-default]")
		return 2
	}
	name := args[0]
	rest := append([]string{}, args[1:]...)
	adapter, rest := takeOption(rest, "--adapter")
	baseURL, rest := takeOption(rest, "--base-url")
	apiKey, rest := takeOption(rest, "--api-key")
	transport, rest := takeOption(rest, "--api-key-transport")
	defaultModel, rest := takeOption(rest, "--default-model")
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(rest, " "))
		return 2
	}
	if !validProviderName(name) {
		fmt.Fprintf(stderr, "benes: invalid provider name %q\n", name)
		return 2
	}
	if adapter == "" || baseURL == "" {
		fmt.Fprintln(stderr, "benes: --adapter and --base-url are required")
		return 2
	}
	if transport != "" && transport != "x-api-key" && transport != "bearer" {
		fmt.Fprintln(stderr, `benes: --api-key-transport must be "x-api-key" or "bearer"`)
		return 2
	}
	providers, defaultName, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	if _, exists := providers[name]; exists && !force {
		fmt.Fprintf(stderr, "benes: provider %q already exists. Use --force to overwrite.\n", name)
		return 1
	}
	rec := providerRecord{
		Adapter:             adapter,
		BaseURL:             baseURL,
		APIKey:              apiKey,
		APIKeyTransport:     transport,
		DefaultModel:        defaultModel,
		AllowPrivateNetwork: allowPrivate,
	}
	encoded, err := json.Marshal(rec)
	if err != nil {
		return serveFailure(stderr, "encode provider", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if err := tx.Set(config.JSONPath("providers", name), encoded); err != nil {
			return err
		}
		if setDefault || defaultName == "" {
			def, err := json.Marshal(name)
			if err != nil {
				return err
			}
			return tx.Set(config.JSONPath("defaultProvider"), def)
		}
		return nil
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{
			"action":    "added",
			"provider":  name,
			"adapter":   adapter,
			"baseUrl":   baseURL,
			"isDefault": setDefault || defaultName == "",
		})
	}
	fmt.Fprintf(stdout, "Provider %q added.\n", name)
	return 0
}

func runProviderRemove(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: provider remove <name>")
		return 2
	}
	name := args[0]
	providers, defaultName, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	if _, ok := providers[name]; !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", name)
		return 1
	}
	if name == defaultName {
		fmt.Fprintf(stderr, "benes: cannot remove %q — it is the default provider\n", name)
		return 1
	}
	if len(providers) <= 1 {
		fmt.Fprintln(stderr, "benes: cannot remove the last provider")
		return 1
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath("providers", name))
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"action": "removed", "provider": name})
	}
	fmt.Fprintf(stdout, "Provider %q removed.\n", name)
	return 0
}

func runProviderShow(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: provider show <name>")
		return 2
	}
	name := args[0]
	providers, defaultName, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	prov, ok := providers[name]
	if !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", name)
		return 1
	}
	masked := maskSecret(prov.APIKey)
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{
			"name":         name,
			"adapter":      prov.Adapter,
			"baseUrl":      prov.BaseURL,
			"apiKey":       masked,
			"defaultModel": prov.DefaultModel,
			"isDefault":    name == defaultName,
		})
	}
	fmt.Fprintf(stdout, "name: %s\nadapter: %s\nbaseUrl: %s\napiKey: %s\ndefaultModel: %s\ndefault: %v\n",
		name, prov.Adapter, prov.BaseURL, masked, prov.DefaultModel, name == defaultName)
	return 0
}

func runProviderSetDefault(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: provider set-default <name>")
		return 2
	}
	name := args[0]
	providers, _, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	if _, ok := providers[name]; !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", name)
		return 1
	}
	encoded, err := json.Marshal(name)
	if err != nil {
		return serveFailure(stderr, "encode default provider", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("defaultProvider"), encoded)
	}); err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "Default provider set to %q.\n", name)
	return 0
}

func runProviderAccountMode(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 || (args[0] != "pool" && args[0] != "direct") {
		fmt.Fprintln(stderr, "benes: usage: provider account-mode <pool|direct>")
		return 2
	}
	mode := args[0]
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	rawProviders, ok := root["providers"]
	if !ok {
		fmt.Fprintln(stderr, "benes: provider \"openai\" is not configured")
		return 1
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal(rawProviders, &blob); err != nil {
		return serveFailure(stderr, "decode providers", err)
	}
	body, ok := blob["openai"]
	if !ok {
		fmt.Fprintln(stderr, "benes: provider \"openai\" is not configured")
		return 1
	}
	var rec map[string]any
	if err := json.Unmarshal(body, &rec); err != nil {
		return serveFailure(stderr, "decode openai provider", err)
	}
	rec["codexAccountMode"] = mode
	encoded, err := json.Marshal(rec)
	if err != nil {
		return serveFailure(stderr, "encode openai provider", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", "openai"), encoded)
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"codexAccountMode": mode})
	}
	fmt.Fprintf(stdout, "OpenAI Codex account mode: %s\n", mode)
	return 0
}

func runProviderEdit(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "benes: usage: provider edit <name> [--adapter <id>] [--base-url <url>] [--default-model <id|->]")
		return 2
	}
	name := args[0]
	rest := append([]string{}, args[1:]...)
	adapter, rest := takeOption(rest, "--adapter")
	baseURL, rest := takeOption(rest, "--base-url")
	defaultModel, rest := takeOption(rest, "--default-model")
	authMode, rest := takeOption(rest, "--auth-mode")
	note, rest := takeOption(rest, "--note")
	transport, rest := takeOption(rest, "--api-key-transport")
	headersRaw, rest := takeOption(rest, "--headers")
	enabledRaw, rest := takeOption(rest, "--enabled")
	liveRaw, rest := takeOption(rest, "--live-models")
	privateRaw, rest := takeOption(rest, "--allow-private-network")
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(rest, " "))
		return 2
	}
	providers, _, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	prov, ok := providers[name]
	if !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", name)
		return 1
	}
	changed := false
	if adapter != "" {
		prov.Adapter = adapter
		changed = true
	}
	if baseURL != "" {
		prov.BaseURL = baseURL
		changed = true
	}
	if defaultModel != "" {
		if defaultModel == "-" {
			prov.DefaultModel = ""
		} else {
			prov.DefaultModel = defaultModel
		}
		changed = true
	}
	if authMode != "" {
		if authMode == "-" {
			prov.AuthMode = ""
		} else {
			prov.AuthMode = authMode
		}
		changed = true
	}
	if note != "" {
		if note == "-" {
			prov.Note = ""
		} else {
			prov.Note = note
		}
		changed = true
	}
	if transport != "" {
		if transport == "-" {
			prov.APIKeyTransport = ""
		} else if transport != "x-api-key" && transport != "bearer" {
			fmt.Fprintln(stderr, `benes: --api-key-transport must be "x-api-key" or "bearer"`)
			return 2
		} else {
			prov.APIKeyTransport = transport
		}
		changed = true
	}
	if headersRaw != "" {
		if headersRaw == "-" {
			prov.Headers = nil
		} else {
			var parsed any
			if err := json.Unmarshal([]byte(headersRaw), &parsed); err != nil {
				fmt.Fprintln(stderr, "benes: --headers must be valid JSON")
				return 2
			}
			if parsed != nil {
				if _, ok := parsed.(map[string]any); !ok {
					fmt.Fprintln(stderr, "benes: --headers must be a JSON object")
					return 2
				}
			}
			prov.Headers = json.RawMessage(headersRaw)
		}
		changed = true
	}
	if enabledRaw != "" {
		on, ok := parseOnOff(enabledRaw)
		if !ok {
			fmt.Fprintln(stderr, "benes: --enabled must be on or off")
			return 2
		}
		prov.Disabled = !on
		changed = true
	}
	if liveRaw != "" {
		on, ok := parseOnOff(liveRaw)
		if !ok {
			fmt.Fprintln(stderr, "benes: --live-models must be on or off")
			return 2
		}
		prov.LiveModels = &on
		changed = true
	}
	if privateRaw != "" {
		on, ok := parseOnOff(privateRaw)
		if !ok {
			fmt.Fprintln(stderr, "benes: --allow-private-network must be on or off")
			return 2
		}
		prov.AllowPrivateNetwork = on
		changed = true
	}
	if !changed {
		fmt.Fprintln(stderr, "benes: at least one edit option is required")
		return 2
	}
	encoded, err := json.Marshal(prov)
	if err != nil {
		return serveFailure(stderr, "encode provider", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", name), encoded)
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"action": "updated", "provider": name})
	}
	fmt.Fprintf(stdout, "Updated provider %s.\n", name)
	return 0
}

func parseOnOff(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1":
		return true, true
	case "off", "false", "0":
		return false, true
	default:
		return false, false
	}
}

func loadProviders(deps commandDependencies) (map[string]providerRecord, string, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, "", err
	}
	out := map[string]providerRecord{}
	if raw, ok := root["providers"]; ok && len(raw) > 0 && string(raw) != "null" {
		var blob map[string]json.RawMessage
		if err := json.Unmarshal(raw, &blob); err != nil {
			return nil, "", err
		}
		for name, body := range blob {
			var rec providerRecord
			if err := json.Unmarshal(body, &rec); err != nil {
				return nil, "", err
			}
			out[name] = rec
		}
	}
	defaultName := ""
	if raw, ok := root["defaultProvider"]; ok {
		_ = json.Unmarshal(raw, &defaultName)
	}
	return out, defaultName, nil
}
func providerNames(providers map[string]providerRecord) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validProviderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func runProviderPresets(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1
	}
	code, payload, err := accessDo(http.MethodGet, base+"/api/provider-presets", nil)
	if err != nil {
		fmt.Fprintf(stderr, "benes: provider presets: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: provider presets failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(payload)
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	var envelope struct {
		Providers []struct {
			ID      string `json:"id"`
			Label   string `json:"label"`
			Adapter string `json:"adapter"`
		} `json:"providers"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		fmt.Fprintln(stderr, "benes: provider presets returned an unexpected payload")
		return 1
	}
	for _, row := range envelope.Providers {
		fmt.Fprintf(stdout, "%s  %s\n", row.ID, strings.TrimSpace(row.Label+"  "+row.Adapter))
	}
	return 0
}

func runProviderTest(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: provider test <name>")
		return 2
	}
	name := args[0]
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1
	}
	code, payload, err := accessDo(http.MethodPost, base+"/api/providers/test?name="+url.QueryEscape(name), nil)
	if err != nil {
		fmt.Fprintf(stderr, "benes: provider test: %v\n", err)
		return 1
	}
	if code == http.StatusNotFound {
		fmt.Fprintf(stderr, "benes: unknown provider %q\n", name)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: provider test failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(payload)
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	var result struct {
		OK         *bool  `json:"ok"`
		Applicable *bool  `json:"applicable"`
		Message    string `json:"message"`
		Error      string `json:"error"`
		LatencyMs  int    `json:"latencyMs"`
	}
	if json.Unmarshal(payload, &result) != nil {
		fmt.Fprintln(stderr, "benes: provider test returned an unexpected payload")
		return 1
	}
	if result.Applicable != nil && !*result.Applicable {
		fmt.Fprintf(stdout, "%s: not applicable\n", name)
		fmt.Fprintln(stdout, "Static catalog; no live model-discovery endpoint to test.")
		return 0
	}
	ok := result.OK != nil && *result.OK
	if ok {
		fmt.Fprintf(stdout, "%s: connected\n", name)
	} else {
		fmt.Fprintf(stdout, "%s: failed\n", name)
	}
	detail := result.Message
	if detail == "" {
		detail = result.Error
	}
	if detail == "" {
		detail = "No detail"
	}
	fmt.Fprintln(stdout, detail)
	fmt.Fprintf(stdout, "Latency: %d ms\n", result.LatencyMs)
	if !ok {
		return 1
	}
	return 0
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}

func encodeJSON(stdout, stderr io.Writer, value any) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		return serveFailure(stderr, "encode json", err)
	}
	return 0
}

func runProviderQuota(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, refresh := takeConfigFlag(args, "--refresh")
	if len(args) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(args, " "))
		return 2
	}
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1
	}
	path := "/api/provider-quotas"
	if refresh {
		path += "?refresh=1"
	}
	code, payload, err := accessDo(http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		fmt.Fprintf(stderr, "benes: provider quota: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: provider quota failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		_, _ = stdout.Write(payload)
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	var envelope struct {
		Reports []struct {
			Provider string `json:"provider"`
			Source   string `json:"source"`
			Quota    struct {
				CustomWindows []struct {
					Label   string  `json:"label"`
					Percent float64 `json:"percent"`
				} `json:"customWindows"`
			} `json:"quota"`
		} `json:"reports"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		fmt.Fprintf(stderr, "benes: decode provider quota: %v\n", err)
		return 1
	}
	if len(envelope.Reports) == 0 {
		fmt.Fprintln(stdout, "no provider quota reports")
		return 0
	}
	for _, row := range envelope.Reports {
		fmt.Fprintf(stdout, "%s  %s\n", row.Provider, row.Source)
		for _, win := range row.Quota.CustomWindows {
			fmt.Fprintf(stdout, "  %s  %.0f%%\n", win.Label, win.Percent)
		}
	}
	return 0
}
