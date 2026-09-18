package server

import (
	"context"

	"github.com/Wibias/Benes/internal/requestpolicy"
)

type admittedServiceTierKey struct{}

// withAdmittedServiceTier freezes the configured request policy for one logical
// request. Every dispatch, retry, and combo attempt for that request reads this value,
// so a live settings change affects only requests admitted afterwards.
func withAdmittedServiceTier(ctx context.Context, configuredServiceTier string) context.Context {
	return context.WithValue(ctx, admittedServiceTierKey{}, configuredServiceTier)
}

// admittedServiceTierFrom returns the tier admitted for this request. A context with no
// admission reports none rather than re-reading live settings.
func admittedServiceTierFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(admittedServiceTierKey{}).(string)
	return value, ok
}

// admittedServiceTier returns the tier admitted for this request scope, or "" when the
// scope admits none. It never reads live settings.
func admittedServiceTier(ctx context.Context) string {
	value, _ := admittedServiceTierFrom(ctx)
	return value
}

// admitServiceTierOnce freezes the configured request policy the first time a request
// scope is created and returns the scope that carries it.
func admitServiceTierOnce(ctx context.Context) context.Context {
	if _, admitted := admittedServiceTierFrom(ctx); admitted {
		return ctx
	}
	return withAdmittedServiceTier(ctx, requestpolicy.AdmittedServiceTier())
}
