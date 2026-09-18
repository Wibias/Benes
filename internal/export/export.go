package export

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Format string

const (
	FormatJSON        Format = "json"
	FormatJSON5       Format = "json5"
	FormatYAML        Format = "yaml"
	FormatTOML        Format = "toml"
	ProviderID               = "benes"
	schemaURL                = "https://opencode.ai/config.json"
	opencodeNPM              = "@ai-sdk/openai-compatible"
	OpenCodeAPIKeyEnv        = "BENES_OPENCODE_API_KEY"
	HermesAPIKeyEnv          = "BENES_HERMES_API_KEY"
	OpenclawAPIKeyEnv        = "BENES_OPENCLAW_API_KEY"
	GajaeAPIKeyEnv           = "BENES_GAJAE_API_KEY"
	loopbackKey              = "benes-loopback"
	outputBudget             = 32000
	piAPI                    = "openai-completions"
)

var MediaType = map[Format]string{
	FormatJSON:  "application/json",
	FormatJSON5: "application/json5",
	FormatYAML:  "application/yaml",
	FormatTOML:  "application/toml",
}

type Model struct {
	Namespaced             string
	Provider               string
	ID                     string
	Native                 bool
	DisplayName            string
	ContextWindow          int
	InputModalities        []string
	ReasoningEfforts       []string
	DefaultReasoningEffort string
}

type Context struct {
	BaseURL  string
	Models   []Model
	Hostname string
	Direct   bool
}

type Result struct {
	Client              string
	Filename            string
	Destination         string
	APIKeyEnv           string
	ExportHint          string
	Format              Format
	MediaType           string
	Text                string
	Document            any
	ModelCount          int
	ModelsWithoutLimits int
}

type spec struct {
	id           string
	filename     string
	apiKeyEnv    string
	hint         string
	format       Format
	loopbackOnly bool
	destination  func() (string, error)
	build        func(Context) (any, error)
	summarize    func(any) (int, int)
}

var specs = map[string]spec{
	"opencode": {id: "opencode", filename: "opencode.json", apiKeyEnv: OpenCodeAPIKeyEnv, hint: "export " + OpenCodeAPIKeyEnv + "=<your key>", format: FormatJSON, destination: destOpencode, build: buildOpencode, summarize: summarizeOpencode},
	"pi":       {id: "pi", filename: "pi-models.json", hint: "Pi reads a non-secret placeholder from models.json; loopback needs no key.", format: FormatJSON, loopbackOnly: true, destination: destPi, build: buildPi, summarize: summarizePi},
	"prime":    {id: "prime", filename: "prime-models.json", hint: "Prime Agent reads a non-secret placeholder from models.json; loopback needs no key.", format: FormatJSON, loopbackOnly: true, destination: destPrime, build: buildPi, summarize: summarizePi},
	"omp":      {id: "omp", filename: "omp-models.yaml", hint: "OMP reads a non-secret placeholder from models.yml; loopback needs no key.", format: FormatYAML, loopbackOnly: true, destination: destOmp, build: buildOmp, summarize: summarizeOmp},
	"hermes":   {id: "hermes", filename: "hermes-config.yaml", apiKeyEnv: HermesAPIKeyEnv, hint: "export " + HermesAPIKeyEnv + "=<your key>", format: FormatYAML, destination: destHermes, build: buildHermes, summarize: summarizeHermes},
	"openclaw": {id: "openclaw", filename: "openclaw.json5", apiKeyEnv: OpenclawAPIKeyEnv, hint: "export " + OpenclawAPIKeyEnv + "=<your key>", format: FormatJSON5, destination: destOpenclaw, build: buildOpenclaw, summarize: summarizeOpenclaw},
	"kimi":     {id: "kimi", filename: "kimi-config.toml", hint: "Kimi Code reads credentials from its config file; loopback needs no key.", format: FormatTOML, loopbackOnly: true, destination: destKimi, build: buildKimi, summarize: summarizeKimi},
	"gajae":    {id: "gajae", filename: "gajae-models.yaml", apiKeyEnv: GajaeAPIKeyEnv, hint: "export " + GajaeAPIKeyEnv + "=<your key>", format: FormatYAML, loopbackOnly: true, destination: destGajae, build: buildGajae, summarize: summarizeGajae},
	"dsh":      {id: "dsh", filename: "settings.yaml", hint: "DSH uses a non-secret loopback bearer placeholder in settings.yaml; loopback needs no key.", format: FormatYAML, loopbackOnly: true, destination: destDsh, build: buildDsh, summarize: summarizeDsh},
	"mcode":    {id: "mcode", filename: "mcode-config.yaml", hint: "MiniMax Code reads a non-secret placeholder from config.yaml; loopback needs no key.", format: FormatYAML, loopbackOnly: true, destination: destMcode, build: buildMcode, summarize: summarizeMcode},
}

var ClientIDs = []string{"opencode", "pi", "prime", "omp", "hermes", "openclaw", "kimi", "gajae", "dsh", "mcode"}

type ClientInfo struct {
	ID           string
	Filename     string
	Destination  string
	APIKeyEnv    string
	ExportHint   string
	Format       Format
	LoopbackOnly bool
}

func Clients() []ClientInfo {
	out := make([]ClientInfo, 0, len(ClientIDs))
	for _, id := range ClientIDs {
		spec := specs[id]
		dest, _ := spec.destination()
		out = append(out, ClientInfo{
			ID:           spec.id,
			Filename:     spec.filename,
			Destination:  dest,
			APIKeyEnv:    spec.apiKeyEnv,
			ExportHint:   spec.hint,
			Format:       spec.format,
			LoopbackOnly: spec.loopbackOnly,
		})
	}
	return out
}

func Build(client string, ctx Context) (Result, error) {
	spec, ok := specs[client]
	if !ok {
		return Result{}, fmt.Errorf("client must be one of: %s", strings.Join(ClientIDs, ", "))
	}
	if spec.loopbackOnly && !isLoopback(hostFromBase(ctx.BaseURL, ctx.Hostname)) {
		return Result{}, fmt.Errorf("%s export is loopback-only", client)
	}
	dest, err := spec.destination()
	if err != nil {
		return Result{}, err
	}
	doc, err := spec.build(ctx)
	if err != nil {
		return Result{}, err
	}
	stampHarnessIdentity(spec.id, doc)
	text, err := serialize(spec.format, doc)
	if err != nil {
		return Result{}, err
	}
	count, missing := spec.summarize(doc)
	return Result{
		Client:              spec.id,
		Filename:            spec.filename,
		Destination:         dest,
		APIKeyEnv:           spec.apiKeyEnv,
		ExportHint:          spec.hint,
		Format:              spec.format,
		MediaType:           MediaType[spec.format],
		Text:                text,
		Document:            doc,
		ModelCount:          count,
		ModelsWithoutLimits: missing,
	}, nil
}

func Normalize(models []Model) []Model {
	seen := map[string]struct{}{}
	out := make([]Model, 0, len(models))
	for _, model := range models {
		if model.Namespaced == "" {
			continue
		}
		if _, ok := seen[model.Namespaced]; ok {
			continue
		}
		seen[model.Namespaced] = struct{}{}
		out = append(out, model)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Namespaced < out[i].Namespaced {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func label(model Model) string {
	provider := "routed"
	if model.Native {
		provider = "native"
	} else if model.Provider != "" {
		provider = model.Provider
	}
	id := model.ID
	if id == "" {
		id = model.Namespaced
	}
	if model.DisplayName != "" {
		return model.DisplayName + " (" + provider + ")"
	}
	return id + " (" + provider + ")"
}

func contextWindow(model Model) int {
	if model.ContextWindow > 0 {
		return model.ContextWindow
	}
	return 0
}

func outputFor(context int) int {
	if context < outputBudget {
		return context
	}
	return outputBudget
}

func hostFromBase(base, hostname string) string {
	if hostname != "" {
		return hostname
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	if i := strings.IndexAny(trimmed, "/:"); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

func isLoopback(hostname string) bool {
	switch strings.ToLower(strings.TrimSpace(hostname)) {
	case "localhost", "127.0.0.1", "::1", "":
		return true
	default:
		return false
	}
}

func injectHeader(hostname string) bool {
	return !isLoopback(hostname)
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func destOpencode() (string, error) {
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdg == "" {
		xdg = filepath.Join(userHome(), ".config")
	}
	return filepath.Join(xdg, "opencode", "opencode.json"), nil
}

func destPi() (string, error) {
	path, _, err := PiPaths(userHome(), os.Getenv)
	return path, err
}

func destPrime() (string, error) {
	path, _, err := PrimePaths(userHome(), os.Getenv)
	return path, err
}

func destOmp() (string, error) { return filepath.Join(userHome(), ".pi", "models.yml"), nil }

func destHermes() (string, error) {
	home := userHome()
	if runtime.GOOS == "windows" {
		local := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "hermes", "config.yaml"), nil
	}
	return filepath.Join(home, ".hermes", "config.yaml"), nil
}

func destOpenclaw() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")); explicit != "" {
		return explicit, nil
	}
	return filepath.Join(userHome(), ".openclaw", "openclaw.json"), nil
}

func destKimi() (string, error) {
	home := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	if home == "" {
		home = filepath.Join(userHome(), ".kimi-code")
	}
	return filepath.Join(home, "config.toml"), nil
}

func destGajae() (string, error) { return filepath.Join(userHome(), ".gjc", "agent", "models.yml"), nil }

func destDsh() (string, error) { return filepath.Join(userHome(), ".dsh", "settings.yaml"), nil }

func destMcode() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("MINIMAX_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "config.yaml"), nil
	}
	return filepath.Join(userHome(), ".minimax", "config.yaml"), nil
}

func PiPaths(home string, getenv func(string) string) (configPath, detectDir string, err error) {
	return codingAgentDirPaths(home, "PI_CODING_AGENT_DIR", ".pi", getenv)
}

func PrimePaths(home string, getenv func(string) string) (configPath, detectDir string, err error) {
	return codingAgentDirPaths(home, "PRIME_AGENT_CODING_AGENT_DIR", ".prime", getenv)
}

func codingAgentDirPaths(home, envKey, productRoot string, getenv func(string) string) (configPath, detectDir string, err error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	dir := strings.TrimSpace(getenv(envKey))
	if dir == "" {
		return filepath.Join(home, productRoot, "agent", "models.json"), filepath.Join(home, productRoot), nil
	}
	if !filepath.IsAbs(dir) {
		return "", "", fmt.Errorf("%s must be absolute", envKey)
	}
	return filepath.Join(dir, "models.json"), dir, nil
}
