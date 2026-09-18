package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Wibias/Benes/internal/requestpolicy"
)

var ErrConfigTooLarge = errors.New("config file exceeds configured byte limit")

const (
	defaultOpenAIProviderJSON = "{\"adapter\":\"openai-responses\",\"baseUrl\":\"https://chatgpt.com/backend-api/codex\",\"authMode\":\"forward\",\"codexAccountMode\":\"pool\"}"
	defaultDiskConfigJSON     = "{\"providers\":{\"openai\":" + defaultOpenAIProviderJSON + "},\"defaultProvider\":\"openai\"}"
)

type DiskConfigSource string

const (
	DiskConfigSourceFile    DiskConfigSource = "file"
	DiskConfigSourceDefault DiskConfigSource = "default"
)

type DiskConfig struct {
	Providers              map[string]json.RawMessage
	DataPlaneTokens        []string
	CORSAllowOrigins       []string
	CodexAccountNamespaces map[string]string
	Proxy                  string
	NoProxy                string
	ProxyDirectFallback    bool
	WebSearchMaxSearches   int
	RequestPolicy          requestpolicy.Policy
	Raw                    json.RawMessage
	Source                 DiskConfigSource
}

func LoadDiskConfig(path string, maxBytes int64) (DiskConfig, error) {
	if strings.TrimSpace(path) == "" {
		return DiskConfig{}, fmt.Errorf("config path is required")
	}
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := defaultDiskConfig()
			_ = requestpolicy.Publish(cfg.RequestPolicy)
			return cfg, nil
		}
		return DiskConfig{}, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return DiskConfig{}, fmt.Errorf("read config: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return DiskConfig{}, ErrConfigTooLarge
	}
	return decodeDiskConfigBytes(data, DiskConfigSourceFile)
}

func decodeDiskConfigBytes(data []byte, source DiskConfigSource) (DiskConfig, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("root must be an object")
		}
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}

	providers := make(map[string]json.RawMessage)
	if raw, exists := root["providers"]; exists {
		if isJSONNull(raw) {
			return DiskConfig{}, fmt.Errorf("decode config: providers must be an object")
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
			return DiskConfig{}, fmt.Errorf("decode config: providers must be an object")
		}
		for id, providerRaw := range decoded {
			if !rawJSONObject(providerRaw) {
				return DiskConfig{}, fmt.Errorf("decode config: provider %q must be an object", id)
			}
			providers[id] = append(json.RawMessage(nil), providerRaw...)
		}
	}

	corsAllowOrigins, err := loadCORSAllowOrigins(root["corsAllowOrigins"])
	if err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}
	codexAccountNamespaces, err := loadCodexAccountNamespaces(root["codexAccountNamespaces"], providers, root["codexAccounts"])
	if err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}
	proxy, noProxy, err := loadProxyFields(root)
	if err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}
	proxyDirectFallback, err := optionalRootBool(root, "proxyDirectFallback")
	if err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}
	webSearchMaxSearches, err := loadWebSearchMaxSearches(root["webSearchSidecar"])
	if err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}
	requestPolicy, err := requestpolicy.Decode(root["requestPolicy"])
	if err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}
	if err := requestpolicy.Publish(requestPolicy); err != nil {
		return DiskConfig{}, fmt.Errorf("decode config: %w", err)
	}

	return DiskConfig{
		Providers:              providers,
		DataPlaneTokens:        loadDataPlaneTokens(root["apiKeys"]),
		CORSAllowOrigins:       corsAllowOrigins,
		CodexAccountNamespaces: codexAccountNamespaces,
		Proxy:                  proxy,
		NoProxy:                noProxy,
		ProxyDirectFallback:    proxyDirectFallback,
		WebSearchMaxSearches:   webSearchMaxSearches,
		RequestPolicy:          requestPolicy,
		Raw:                    append(json.RawMessage(nil), data...),
		Source:                 source,
	}, nil
}

func defaultDiskConfig() DiskConfig {
	return DiskConfig{
		Providers: map[string]json.RawMessage{
			"openai": json.RawMessage(defaultOpenAIProviderJSON),
		},
		RequestPolicy: requestpolicy.Policy{},
		Raw:           json.RawMessage(defaultDiskConfigJSON),
		Source:        DiskConfigSourceDefault,
	}
}

func loadCORSAllowOrigins(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var origins []string
	if err := json.Unmarshal(raw, &origins); err != nil || origins == nil {
		return nil, fmt.Errorf("corsAllowOrigins must be an array of nonblank strings")
	}
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "" {
			return nil, fmt.Errorf("corsAllowOrigins must contain only nonblank strings")
		}
	}
	return origins, nil
}

func loadProxyFields(root map[string]json.RawMessage) (string, string, error) {
	proxy, err := optionalRootString(root, "proxy")
	if err != nil {
		return "", "", err
	}
	noProxy, err := optionalRootString(root, "noProxy")
	if err != nil {
		if raw, exists := root["noProxy"]; exists {
			var entries []string
			if json.Unmarshal(raw, &entries) == nil && entries != nil {
				return proxy, strings.Join(entries, ","), nil
			}
		}
		return "", "", err
	}
	return proxy, noProxy, nil
}

func optionalRootString(root map[string]json.RawMessage, field string) (string, error) {
	raw, exists := root[field]
	if !exists || len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return "", nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return strings.TrimSpace(value), nil
}

func optionalRootBool(root map[string]json.RawMessage, field string) (bool, error) {
	raw, exists := root[field]
	if !exists || len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean", field)
	}
	return value, nil
}

func loadWebSearchMaxSearches(raw json.RawMessage) (int, error) {
	if len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return 0, nil
	}
	var sidecar map[string]json.RawMessage
	if json.Unmarshal(raw, &sidecar) != nil || sidecar == nil {
		return 0, fmt.Errorf("webSearchSidecar must be an object")
	}
	field, exists := sidecar["maxSearchesPerTurn"]
	if !exists || isJSONNull(field) {
		return 0, nil
	}
	var n float64
	if json.Unmarshal(field, &n) != nil {
		return 0, fmt.Errorf("webSearchSidecar.maxSearchesPerTurn must be a number")
	}
	if n != float64(int(n)) || n < 0 {
		return 0, fmt.Errorf("webSearchSidecar.maxSearchesPerTurn must be a non-negative integer")
	}
	return int(n), nil
}

func loadDataPlaneTokens(raw json.RawMessage) []string {
	if len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return nil
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	var tokens []string
	for _, rowRaw := range rows {
		var row map[string]json.RawMessage
		if json.Unmarshal(rowRaw, &row) != nil || row == nil {
			continue
		}
		var key string
		if json.Unmarshal(row["key"], &key) != nil || key == "" || key != strings.TrimSpace(key) {
			continue
		}
		tokens = append(tokens, key)
	}
	return tokens
}

func rawJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var value map[string]json.RawMessage
	return json.Unmarshal(trimmed, &value) == nil && value != nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
