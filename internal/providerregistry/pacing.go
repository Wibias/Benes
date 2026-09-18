package providerregistry

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

func applySpecPacing(spec Spec, options transport.ClientOptions, runtime map[string]*transport.Pacer) (transport.ClientOptions, bool) {
	options = applySpecRequestBodyLimitScope(spec, options)
	if spec.RequestPacing <= 0 && len(spec.ModelRequestPacing) == 0 {
		return options, false
	}
	base := spec.RequestPacing
	models := clonePacingMap(spec.ModelRequestPacing)
	pacer := transport.NewPacer(base, nil)
	options.Pacer = pacer
	options.PaceKey = func(*http.Request) string { return spec.ID }
	options.PaceInterval = func(request *http.Request) time.Duration {
		label := ""
		if request != nil {
			label = transport.PacingLabel(request.Context())
		}
		return pacingIntervalForModel(spec.ID, label, base, models)
	}
	options.PaceLabel = func(request *http.Request) string {
		if request == nil {
			return ""
		}
		return transport.PacingLabel(request.Context())
	}
	if runtime != nil {
		runtime[spec.ID] = pacer
	}
	return options, true
}

func pacingIntervalForModel(providerID, label string, base time.Duration, models map[string]time.Duration) time.Duration {
	label = strings.TrimSpace(label)
	if label == "" || len(models) == 0 {
		return base
	}
	if interval, ok := models[label]; ok {
		return interval
	}
	trimmed := strings.TrimPrefix(label, strings.TrimSpace(providerID)+"/")
	if interval, ok := models[trimmed]; ok {
		return interval
	}
	return base
}

func clonePacingMap(source map[string]time.Duration) map[string]time.Duration {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]time.Duration, len(source))
	for model, interval := range source {
		out[model] = interval
	}
	return out
}

type pacingLabelProvider struct{ next providers.Responses }

func (p pacingLabelProvider) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	label := strings.TrimSpace(dispatch.Parsed.UpstreamModelID)
	if label == "" {
		label = strings.TrimSpace(dispatch.Parsed.ModelID)
	}
	return p.next.Open(transport.WithPacingLabel(ctx, label), dispatch)
}

func withPacingLabel(provider providers.Responses, enabled bool) providers.Responses {
	if !enabled || provider == nil {
		return provider
	}
	return pacingLabelProvider{next: provider}
}
