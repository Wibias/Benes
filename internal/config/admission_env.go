package config

import (
	"os"
	"strings"
)

const BenesAPIAuthTokenEnv = "BENES_API_AUTH_TOKEN"

func ResolveEnvironmentDataPlaneToken(env map[string]string) string {
	lookup := func(key string) string {
		if env != nil {
			return env[key]
		}
		return os.Getenv(key)
	}
	return strings.TrimSpace(lookup(BenesAPIAuthTokenEnv))
}
