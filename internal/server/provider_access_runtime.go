package server

import (
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/config"
)

// providerDefaultAccessCache keeps the management-config preference off the
// per-request disk path. Dashboard mutations invalidate the entry through the
// provider activity hook after the config transaction commits.
var providerDefaultAccessCache sync.Map // map[string]string, keyed by config path

func (h *handler) logicalOpenAIDefaultAccess() string {
	if h == nil {
		return config.DefaultAccessOAuth
	}
	path := strings.TrimSpace(h.configPath)
	if path == "" {
		return config.DefaultAccessOAuth
	}
	if cached, ok := providerDefaultAccessCache.Load(path); ok {
		if value, ok := cached.(string); ok && value != "" {
			return value
		}
	}

	value := config.DefaultAccessOAuth
	if disk, err := config.LoadDiskConfig(path, 0); err == nil {
		value = config.OpenAIDefaultAccess(disk)
	}
	providerDefaultAccessCache.Store(path, value)
	return value
}

func invalidateProviderDefaultAccess(configPath string) {
	if path := strings.TrimSpace(configPath); path != "" {
		providerDefaultAccessCache.Delete(path)
	}
}
