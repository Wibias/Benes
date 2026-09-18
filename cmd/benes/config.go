package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
)

var configSecretKey = regexp.MustCompile(`(?i)^(apiKey|key|accessToken|refreshToken|idToken|token|password|clientSecret)$`)

const configUsage = `Usage:
  benes config show [--json]
  benes config get <dot.path> [--json]
  benes config set <dot.path> <json-or-string>
  benes config unset <dot.path>
  benes config mutations [--limit N] [--json]
  benes config validate [path]
  benes config export <path|->
  benes config import <path|-> --yes
  benes config set-context-window <provider> <model> <tokens>`

func runConfig(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, yes := takeConfigFlag(args, "--yes")
	args, _ = takeConfigFlag(args, "--json")
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, configUsage)
		return 0
	}
	switch args[0] {
	case "show":
		return runConfigShow(stdout, stderr, deps)
	case "get":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "benes: usage: config get <dot.path>")
			return 2
		}
		return runConfigGet(args[1], stdout, stderr, deps)
	case "set":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "benes: usage: config set <dot.path> <json-or-string>")
			return 2
		}
		return runConfigSet(args[1], args[2], stdout, stderr, deps)
	case "unset":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "benes: usage: config unset <dot.path>")
			return 2
		}
		return runConfigUnset(args[1], stdout, stderr, deps)
	case "mutations":
		return runConfigMutations(args[1:], stdout, stderr, deps)
	case "validate":
		path := ""
		if len(args) == 2 {
			path = args[1]
		} else if len(args) != 1 {
			fmt.Fprintln(stderr, "benes: usage: config validate [path]")
			return 2
		}
		return runConfigValidate(path, stdout, stderr, deps)
	case "export":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "benes: usage: config export <path|->")
			return 2
		}
		return runConfigExport(args[1], stdout, stderr, deps)
	case "import":
		if !yes || len(args) != 2 {
			fmt.Fprintln(stderr, "benes: usage: config import <path|-> --yes")
			return 2
		}
		return runConfigImport(args[1], stdout, stderr, deps)
	case "set-context-window":
		return runConfigSetContextWindow(args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, configUsage)
		return 2
	}
}

func runConfigMutations(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	limit := 50
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--limit":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: usage: config mutations [--limit N] [--json]")
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				fmt.Fprintln(stderr, "benes: usage: config mutations [--limit N] [--json]")
				return 2
			}
			limit = n
			i++
		default:
			fmt.Fprintln(stderr, "benes: usage: config mutations [--limit N] [--json]")
			return 2
		}
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	records, err := config.ListMutations(paths.Config, limit)
	if err != nil {
		return serveFailure(stderr, "list config mutations", err)
	}
	if records == nil {
		records = []config.MutationRecord{}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(records); err != nil {
		return serveFailure(stderr, "encode config mutations", err)
	}
	return 0
}

func runConfigShow(stdout, stderr io.Writer, deps commandDependencies) int {
	root, err := loadConfigObject(stderr, deps)
	if err != nil {
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(redactConfigValue(root, "")); err != nil {
		return serveFailure(stderr, "encode config", err)
	}
	return 0
}

func runConfigGet(path string, stdout, stderr io.Writer, deps commandDependencies) int {
	root, err := loadConfigObject(stderr, deps)
	if err != nil {
		return 1
	}
	value, err := configPath(root, path)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	redacted := redactConfigValue(value, lastPathSegment(path))
	if err := json.NewEncoder(stdout).Encode(redacted); err != nil {
		return serveFailure(stderr, "encode config path", err)
	}
	return 0
}

func runConfigSet(path, raw string, stdout, stderr io.Writer, deps commandDependencies) int {
	encoded, err := parseConfigValue(raw)
	if err != nil {
		fmt.Fprintf(stderr, "benes: encode config value: %v\n", err)
		return 2
	}
	segments, err := configPathSegments(path)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath(segments...), encoded)
	}); err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "Set %s.\n", path)
	return 0
}

func runConfigUnset(path string, stdout, stderr io.Writer, deps commandDependencies) int {
	segments, err := configPathSegments(path)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath(segments...))
	}); err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "Unset %s.\n", path)
	return 0
}

func runConfigValidate(path string, stdout, stderr io.Writer, deps commandDependencies) int {
	if path == "" {
		resolved, err := deps.resolvePaths(config.PathOptions{})
		if err != nil {
			return serveFailure(stderr, "resolve paths", err)
		}
		path = resolved.Config
	}
	if _, err := config.LoadDiskConfig(path, 0); err != nil {
		fmt.Fprintf(stderr, "Config is invalid: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Config is valid.")
	return 0
}

func runConfigExport(target string, stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	content := append([]byte(nil), disk.Raw...)
	if len(content) == 0 || content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	if target == "-" {
		_, err = stdout.Write(content)
		if err != nil {
			return serveFailure(stderr, "write export", err)
		}
		return 0
	}
	if err := os.WriteFile(target, content, 0o600); err != nil {
		return serveFailure(stderr, "write export", err)
	}
	fmt.Fprintf(stdout, "Exported config to %s.\n", target)
	return 0
}

func runConfigImport(source string, stdout, stderr io.Writer, deps commandDependencies) int {
	raw, err := readConfigInput(source)
	if err != nil {
		return serveFailure(stderr, "read import", err)
	}
	var incoming map[string]json.RawMessage
	if err := json.Unmarshal(raw, &incoming); err != nil || incoming == nil {
		fmt.Fprintln(stderr, "benes: import must be a JSON object")
		return 2
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		current, loadErr := loadConfigRawObject(deps)
		if loadErr != nil {
			return loadErr
		}
		for key := range current {
			if _, keep := incoming[key]; !keep {
				if err := tx.Delete(config.JSONPath(key)); err != nil {
					return err
				}
			}
		}
		for key, value := range incoming {
			if err := tx.Set(config.JSONPath(key), value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "Imported config from %s. Restart or run benes sync if needed.\n", source)
	return 0
}

func runConfigSetContextWindow(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 3 {
		fmt.Fprintln(stderr, "benes: usage: benes config set-context-window <provider> <model> <tokens>")
		return 2
	}
	tokens, err := strconv.Atoi(strings.TrimSpace(args[2]))
	if err != nil || tokens <= 0 {
		fmt.Fprintln(stderr, "benes: context window tokens must be a positive integer")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	store := catalog.PolicyStore{Transactions: config.NewTransactionStore(paths.Config, 0)}
	tx, err := store.Begin()
	if err != nil {
		return serveFailure(stderr, "begin config transaction", err)
	}
	if err := tx.SetContextWindow(args[0], args[1], tokens, catalog.WindowOverride); err != nil {
		return serveFailure(stderr, "set context window", err)
	}
	if _, err := tx.Commit(); err != nil {
		return serveFailure(stderr, "commit config transaction", err)
	}
	fmt.Fprintf(stdout, "set %s/%s context window to %d\n", args[0], args[1], tokens)
	return 0
}

func mutateConfig(stderr io.Writer, deps commandDependencies, op func(*config.Transaction) error) error {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		serveFailure(stderr, "resolve paths", err)
		return err
	}
	tx, err := config.NewTransactionStore(paths.Config, 0).Begin()
	if err != nil {
		serveFailure(stderr, "begin config transaction", err)
		return err
	}
	tx.SetSource(config.MutationSource{Class: config.SourceCLI, Detail: "cli"})
	if err := op(tx); err != nil {
		serveFailure(stderr, "mutate config", err)
		return err
	}
	if _, err := tx.Commit(); err != nil {
		serveFailure(stderr, "commit config transaction", err)
		return err
	}
	return nil
}

func loadConfigObject(stderr io.Writer, deps commandDependencies) (any, error) {
	raw, err := loadConfigRawObject(deps)
	if err != nil {
		serveFailure(stderr, "load config", err)
		return nil, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		serveFailure(stderr, "decode config", err)
		return nil, err
	}
	var root any
	if err := json.Unmarshal(encoded, &root); err != nil {
		serveFailure(stderr, "decode config", err)
		return nil, err
	}
	return root, nil
}

func loadConfigRawObject(deps commandDependencies) (map[string]json.RawMessage, error) {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return nil, err
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(disk.Raw, &root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("root must be an object")
		}
		return nil, err
	}
	return root, nil
}

func readConfigInput(source string) ([]byte, error) {
	if source == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(source)
}

func parseConfigValue(raw string) (json.RawMessage, error) {
	trim := strings.TrimSpace(raw)
	if json.Valid([]byte(trim)) {
		return json.RawMessage(trim), nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func takeConfigFlag(args []string, name string) ([]string, bool) {
	found := false
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return out, found
}

func configPath(root any, path string) (any, error) {
	current := root
	segments, err := configPathSegments(path)
	if err != nil {
		return nil, err
	}
	for _, segment := range segments {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config path not found: %s", path)
		}
		next, ok := obj[segment]
		if !ok {
			return nil, fmt.Errorf("config path not found: %s", path)
		}
		current = next
	}
	return current, nil
}

func configPathSegments(path string) ([]string, error) {
	out := make([]string, 0)
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "__proto__" || segment == "prototype" || segment == "constructor" {
			return nil, fmt.Errorf("invalid config path")
		}
		out = append(out, segment)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("invalid config path")
	}
	return out, nil
}

func lastPathSegment(path string) string {
	parts := strings.Split(path, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}

func redactConfigValue(value any, key string) any {
	if s, ok := value.(string); ok && configSecretKey.MatchString(key) {
		if s == "" {
			return s
		}
		return "********"
	}
	switch typed := value.(type) {
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactConfigValue(item, "")
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, child := range typed {
			if strings.EqualFold(key, "modelCosts") && strings.HasPrefix(strings.ToLower(childKey), "sk-") {
				continue
			}
			out[childKey] = redactConfigValue(child, childKey)
		}
		return out
	default:
		return value
	}
}
