package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
)

//go:embed init_providers.json
var initProvidersJSON []byte

type initProvider struct {
	ID               string          `json:"id"`
	Label            string          `json:"label"`
	Adapter          string          `json:"adapter"`
	BaseURL          string          `json:"baseUrl"`
	Kind             string          `json:"kind"`
	CodexAccountMode string          `json:"codexAccountMode"`
	DashboardURL     string          `json:"dashboardUrl"`
	DefaultModel     string          `json:"defaultModel"`
	Seed             json.RawMessage `json:"seed"`
}

func loadInitProviders() ([]initProvider, error) {
	var envelope struct {
		Providers []initProvider `json:"providers"`
	}
	if err := json.Unmarshal(initProvidersJSON, &envelope); err != nil {
		return nil, err
	}
	return envelope.Providers, nil
}

func runInit(args []string, stdin io.Reader, stdout, stderr io.Writer, deps commandDependencies) int {
	providerID, port := "", 23100
	apiKey, baseURL := "", ""
	noInject, noShim := false, false
	rest := args
	for len(rest) > 0 {
		switch rest[0] {
		case "--provider":
			if len(rest) < 2 {
				fmt.Fprintln(stderr, "benes: usage: init [--provider <id>] [--port <n>] [--api-key <key>] [--base-url <url>] [--no-inject] [--no-shim]")
				return 2
			}
			providerID = rest[1]
			rest = rest[2:]
		case "--port":
			if len(rest) < 2 {
				fmt.Fprintln(stderr, "benes: --port requires a value")
				return 2
			}
			n, err := strconv.Atoi(rest[1])
			if err != nil || n <= 0 {
				fmt.Fprintln(stderr, "benes: --port must be a positive integer")
				return 2
			}
			port = n
			rest = rest[2:]
		case "--api-key":
			if len(rest) < 2 {
				fmt.Fprintln(stderr, "benes: --api-key requires a value")
				return 2
			}
			apiKey = rest[1]
			rest = rest[2:]
		case "--base-url":
			if len(rest) < 2 {
				fmt.Fprintln(stderr, "benes: --base-url requires a value")
				return 2
			}
			baseURL = rest[1]
			rest = rest[2:]
		case "--no-inject":
			noInject = true
			rest = rest[1:]
		case "--no-shim":
			noShim = true
			rest = rest[1:]
		default:
			fmt.Fprintf(stderr, "benes: unexpected argument %q\n", rest[0])
			return 2
		}
	}
	providers, err := loadInitProviders()
	if err != nil {
		fmt.Fprintf(stderr, "benes: load init providers: %v\n", err)
		return 1
	}
	if providerID != "" {
		return runInitSelected(providerID, port, apiKey, baseURL, noInject, noShim, stdout, stderr, deps, providers)
	}
	return runInitInteractive(stdin, stdout, stderr, deps, providers)
}

func runInitSelected(id string, port int, apiKey, baseURL string, noInject, noShim bool, stdout, stderr io.Writer, deps commandDependencies, providers []initProvider) int {
	var selected *initProvider
	for i := range providers {
		if providers[i].ID == id {
			selected = &providers[i]
			break
		}
	}
	if selected == nil {
		fmt.Fprintf(stderr, "benes: unknown init provider %q\n", id)
		return 2
	}
	seed, err := initSeed(*selected, apiKey, baseURL)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	if err := writeInitConfig(port, selected.ID, seed, stderr, deps); err != nil {
		return 1
	}
	fmt.Fprintln(stdout, "Config saved.")
	if selected.Kind == "oauth" {
		fmt.Fprintf(stdout, "Authenticate this provider with: benes login %s\n", selected.ID)
	}
	if !noInject {
		warnInitInject(port, stdout, stderr)
	}
	if !noShim {
		_ = runCodexShim([]string{"install"}, stdout, stderr, deps)
	}
	fmt.Fprintln(stdout, "Setup complete. Run 'benes start' to start the proxy.")
	return 0
}

func runInitInteractive(stdin io.Reader, stdout, stderr io.Writer, deps commandDependencies, providers []initProvider) int {
	if stdin == nil {
		stdin = os.Stdin
	}
	sc := bufio.NewScanner(stdin)
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "🔧 benes (benes) setup")
	fmt.Fprintln(stdout, "")
	printInitMenu(stdout, providers)
	choice, ok := readInitLine(sc, stderr, "\nSelect default provider (number): ")
	if !ok {
		return 1
	}
	idx, _ := strconv.Atoi(strings.TrimSpace(choice))
	idx--
	var (
		name string
		seed json.RawMessage
		kind string
	)
	if idx >= 0 && idx < len(providers) {
		p := providers[idx]
		name = p.ID
		kind = p.Kind
		fmt.Fprintf(stdout, "\n📡 %s\n", p.Label)
		fmt.Fprintf(stdout, "   Base URL: %s\n", p.BaseURL)
		apiKey, baseURL := "", ""
		switch p.Kind {
		case "forward":
			fmt.Fprintln(stdout, "   No API key needed — forwards your existing `codex login`.")
		case "oauth":
			// login happens after save
		default:
			if p.DashboardURL != "" {
				fmt.Fprintf(stdout, "   🔑 Get your key: %s\n", p.DashboardURL)
			}
			baseURL = p.BaseURL
			if strings.Contains(baseURL, "{") {
				line, ok := readInitLine(sc, stderr, fmt.Sprintf("   Your endpoint URL (%s): ", baseURL))
				if !ok {
					return 1
				}
				baseURL = strings.TrimSpace(line)
				if baseURL == "" {
					fmt.Fprintln(stderr, "   A resolved URL is required — replace the {placeholder} with your actual value.")
					return 1
				}
			}
			hint := "API key (paste, or env var): "
			if p.Kind == "local" {
				hint = "API key (usually blank — press Enter): "
			}
			line, ok := readInitLine(sc, stderr, "\n"+hint)
			if !ok {
				return 1
			}
			apiKey = strings.TrimSpace(line)
			modelHint := "Default model (optional): "
			if p.DefaultModel != "" {
				modelHint = fmt.Sprintf("Default model [%s]: ", p.DefaultModel)
			}
			if _, ok = readInitLine(sc, stderr, modelHint); !ok {
				return 1
			}
		}
		var err error
		seed, err = initSeed(p, apiKey, baseURL)
		if err != nil {
			fmt.Fprintf(stderr, "benes: %v\n", err)
			return 1
		}
	} else {
		line, ok := readInitLine(sc, stderr, "Provider name: ")
		if !ok {
			return 1
		}
		name = strings.TrimSpace(line)
		if !validProviderName(name) {
			fmt.Fprintln(stderr, "Provider name must use letters, numbers, dot, underscore, or hyphen and cannot be a reserved object key.")
			return 1
		}
		base, ok := readInitLine(sc, stderr, "Base URL (e.g. http://localhost:11434/v1): ")
		if !ok {
			return 1
		}
		adapter, ok := readInitLine(sc, stderr, "Adapter [openai-chat]: ")
		if !ok {
			return 1
		}
		if strings.TrimSpace(adapter) == "" {
			adapter = "openai-chat"
		}
		key, ok := readInitLine(sc, stderr, "API key (optional): ")
		if !ok {
			return 1
		}
		model, ok := readInitLine(sc, stderr, "Default model: ")
		if !ok {
			return 1
		}
		rec := map[string]any{"adapter": strings.TrimSpace(adapter), "baseUrl": strings.TrimSpace(base)}
		if strings.TrimSpace(key) != "" {
			rec["apiKey"] = strings.TrimSpace(key)
		}
		if strings.TrimSpace(model) != "" {
			rec["defaultModel"] = strings.TrimSpace(model)
		}
		seed, _ = json.Marshal(rec)
	}
	portLine, ok := readInitLine(sc, stderr, "\nProxy port [23100]: ")
	if !ok {
		return 1
	}
	port := 23100
	if n, err := strconv.Atoi(strings.TrimSpace(portLine)); err == nil && n > 0 {
		port = n
	}
	if err := writeInitConfig(port, name, seed, stderr, deps); err != nil {
		return 1
	}
	fmt.Fprintln(stdout, "\n✅ Config saved to ~/.benes/config.json")
	if kind == "oauth" {
		fmt.Fprintf(stdout, "🔐 Authenticate this provider with:  benes login %s\n", name)
	}
	injectAnswer, ok := readInitLine(sc, stderr, "Inject into Codex config.toml? [Y/n]: ")
	if !ok {
		return 1
	}
	if strings.ToLower(strings.TrimSpace(injectAnswer)) != "n" {
		warnInitInject(port, stdout, stderr)
	}
	shimAnswer, ok := readInitLine(sc, stderr, "Install Codex autostart shim? [Y/n]: ")
	if !ok {
		return 1
	}
	if strings.ToLower(strings.TrimSpace(shimAnswer)) != "n" {
		_ = runCodexShim([]string{"install"}, stdout, stderr, deps)
	}
	fmt.Fprintln(stdout, "\n🚀 Setup complete! Run 'benes start' to start the proxy.")
	return 0
}

func printInitMenu(stdout io.Writer, providers []initProvider) {
	fmt.Fprintln(stdout, "Choose your default provider (you can add more later):")
	lastKind := ""
	headings := map[string]string{
		"forward": "ChatGPT login",
		"oauth":   "Account login (OAuth — then run: benes login <id>)",
		"key":     "API key (paste a key from the provider's dashboard)",
		"local":   "Local servers (usually no key)",
	}
	for i, p := range providers {
		if p.Kind != lastKind {
			fmt.Fprintf(stdout, "\n  %s:\n", headings[p.Kind])
			lastKind = p.Kind
		}
		fmt.Fprintf(stdout, "   %2d. %s\n", i+1, p.Label)
	}
	fmt.Fprintf(stdout, "\n   %d. custom (enter URL manually)\n", len(providers)+1)
}

func readInitLine(sc *bufio.Scanner, stderr io.Writer, prompt string) (string, bool) {
	fmt.Fprint(os.Stdout, prompt)
	if prompt != "" && !strings.HasSuffix(prompt, "\n") {
		// prompt already printed
	}
	if !sc.Scan() {
		fmt.Fprintln(stderr, "stdin reached EOF while waiting for input. Re-run `benes init` in an interactive terminal.")
		return "", false
	}
	return sc.Text(), true
}

func initSeed(p initProvider, apiKey, baseURL string) (json.RawMessage, error) {
	var seed map[string]any
	if len(p.Seed) > 0 {
		if err := json.Unmarshal(p.Seed, &seed); err != nil {
			return nil, fmt.Errorf("decode provider seed: %w", err)
		}
	} else {
		seed = map[string]any{"adapter": p.Adapter, "baseUrl": p.BaseURL}
	}
	if baseURL != "" {
		seed["baseUrl"] = baseURL
	}
	if strings.Contains(fmt.Sprint(seed["baseUrl"]), "{") {
		return nil, fmt.Errorf("a resolved URL is required — replace the {placeholder} with your actual value")
	}
	if p.Kind == "key" {
		if apiKey != "" {
			seed["apiKey"] = apiKey
		} else {
			env := strings.ToUpper(p.ID)
			env = strings.Map(func(r rune) rune {
				if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
					return r
				}
				return '_'
			}, env)
			seed["apiKey"] = "${" + env + "_API_KEY}"
		}
	} else if apiKey != "" {
		seed["apiKey"] = apiKey
	}
	return json.Marshal(seed)
}
func writeInitConfig(port int, name string, seed json.RawMessage, stderr io.Writer, deps commandDependencies) error {
	portJSON, _ := json.Marshal(port)
	nameJSON, _ := json.Marshal(name)
	tierJSON, _ := json.Marshal(2)
	trueJSON, _ := json.Marshal(true)
	falseJSON, _ := json.Marshal(false)
	providersJSON, err := json.Marshal(map[string]json.RawMessage{name: seed})
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if err := tx.Set(config.JSONPath("port"), portJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("openaiProviderTierVersion"), tierJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("emptyCompletionRetry"), falseJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("websockets"), falseJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("codexAutoStart"), trueJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("codexShimAutoRestore"), trueJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("multiAgentGuidanceEnabled"), trueJSON); err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("providers"), providersJSON); err != nil {
			return err
		}
		return tx.Set(config.JSONPath("defaultProvider"), nameJSON)
	})
}

func warnInitInject(port int, stdout, stderr io.Writer) {
	fmt.Fprintln(stdout, "Fetching available models from provider...")
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stdout, "⚠️  Codex inject skipped: %v\n", err)
		return
	}
	result, err := codexrestore.Inject(home, fmt.Sprintf("http://127.0.0.1:%d/v1", port))
	if err != nil {
		fmt.Fprintf(stdout, "⚠️  %v\n", err)
		return
	}
	fmt.Fprintf(stdout, "✅ %s\n", result.Message)
	_ = stderr
}
