package server

import "github.com/Wibias/Benes/internal/router"

// webSearchProviderID keeps hosted web-search sidecars on the same physical
// provider connection selected for the request. In particular, logical OpenAI
// must not dispatch the main request through openai-apikey while resolving the
// sidecar from the OAuth/openai configuration, and exact-account selectors must
// remain on their explicit OAuth connection.
func webSearchProviderID(route router.Route, resolved resolvedProvider) string {
	if resolved.ProviderID != "" {
		return resolved.ProviderID
	}
	return route.Provider
}
