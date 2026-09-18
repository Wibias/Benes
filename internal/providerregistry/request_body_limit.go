package providerregistry

import (
	"net/url"

	"github.com/Wibias/Benes/internal/transport"
)

func applySpecRequestBodyLimitScope(spec Spec, options transport.ClientOptions) transport.ClientOptions {
	if spec.MaxUpstreamBodyBytes <= 0 {
		return options
	}
	if endpoint, err := url.Parse(spec.Endpoint); err == nil {
		path := endpoint.Path
		if path == "" {
			path = "/"
		}
		options.RequestBodyLimitPath = path
	}
	return options
}
