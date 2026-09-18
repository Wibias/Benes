package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/harnessidentity"
)

type harnessIdentityStatus string

const (
	harnessIdentityMissing  harnessIdentityStatus = "missing"
	harnessIdentityAccepted harnessIdentityStatus = "accepted"
	harnessIdentityRejected harnessIdentityStatus = "rejected"
)

type harnessRequestIdentity struct {
	ID     string
	Status harnessIdentityStatus
}

type harnessIdentityContextKey struct{}

func parseHarnessIdentity(values []string) harnessRequestIdentity {
	if len(values) == 0 {
		return harnessRequestIdentity{Status: harnessIdentityMissing}
	}
	if len(values) != 1 {
		return harnessRequestIdentity{Status: harnessIdentityRejected}
	}
	id := strings.TrimSpace(values[0])
	if id == "" || strings.Contains(id, ",") {
		return harnessRequestIdentity{Status: harnessIdentityRejected}
	}
	caps, ok := harnessSidecarCapabilitiesFor(id)
	if !ok || !caps.IdentityStampable {
		return harnessRequestIdentity{Status: harnessIdentityRejected}
	}
	return harnessRequestIdentity{ID: id, Status: harnessIdentityAccepted}
}

func bindHarnessIdentity(r *http.Request) *http.Request {
	if r == nil {
		return r
	}
	identity := parseHarnessIdentity(r.Header.Values(harnessidentity.Header))
	r.Header.Del(harnessidentity.Header)
	ctx := context.WithValue(r.Context(), harnessIdentityContextKey{}, identity)
	return r.WithContext(ctx)
}

func harnessIdentityFromContext(ctx context.Context) harnessRequestIdentity {
	if ctx == nil {
		return harnessRequestIdentity{Status: harnessIdentityMissing}
	}
	identity, ok := ctx.Value(harnessIdentityContextKey{}).(harnessRequestIdentity)
	if !ok {
		return harnessRequestIdentity{Status: harnessIdentityMissing}
	}
	return identity
}
