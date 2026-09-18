package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/harnessidentity"
)

const defaultCORSOrigin = "http://localhost:23100"

const staticAllowedRequestHeaders = "Content-Type, Authorization, X-Benes-API-Key, X-Api-Key, Anthropic-Version, Anthropic-Beta, ChatGPT-Account-Id, OpenAI-Alpha, X-Session-Id, Session-Id, Thread-Id, Originator, X-OAI-Attestation, " + harnessidentity.Header

func normalizeCORSDefaultOrigin(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultCORSOrigin, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("CORS default origin must be an HTTP(S) origin")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func (a dataPlaneAdmission) allowsRequestOrigin(r requestView) bool {
	switch a.kind {
	case admissionLegacyBearer:
		return true
	case admissionLoopback:
		return isLoopbackRequestHost(r.host) && originAllowed(r, true, a.corsAllowOrigins)
	case admissionRemote:
		return originAllowed(r, false, a.corsAllowOrigins)
	default:
		return false
	}
}

func requestViewFromHTTP(r *http.Request) requestView {
	return requestView{
		host:          r.Host,
		origin:        r.Header.Get("Origin"),
		authorization: r.Header.Get("Authorization"),
		dedicatedKey:  r.Header.Get("X-Benes-API-Key"),
		tls:           r.TLS != nil,
	}
}

func applyCORSHeaders(header http.Header, r *http.Request, admission dataPlaneAdmission) {
	origin := r.Header.Get("Origin")
	originAllowed := origin != "" && admission.allowsRequestOrigin(requestViewFromHTTP(r))
	allowOrigin := admission.corsDefaultOrigin
	if allowOrigin == "" {
		allowOrigin = defaultCORSOrigin
	}
	if originAllowed {
		allowOrigin = origin
	}

	header.Set("Access-Control-Allow-Origin", allowOrigin)
	header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	header.Set("Access-Control-Allow-Headers", allowedRequestHeaders(r, originAllowed))
	header.Set("Vary", "Origin, Access-Control-Request-Headers")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Content-Security-Policy", "frame-ancestors 'none'")
}

func allowedRequestHeaders(r *http.Request, originAllowed bool) string {
	if !originAllowed {
		return staticAllowedRequestHeaders
	}
	requested := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
	if requested == "" {
		return staticAllowedRequestHeaders
	}

	seen := make(map[string]struct{})
	for _, value := range strings.Split(staticAllowedRequestHeaders, ",") {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	extra := make([]string, 0)
	for _, raw := range strings.Split(requested, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		extra = append(extra, name)
	}
	if len(extra) == 0 {
		return staticAllowedRequestHeaders
	}
	return staticAllowedRequestHeaders + ", " + strings.Join(extra, ", ")
}

func (h *handler) handleCORS(w http.ResponseWriter, r *http.Request) bool {
	applyCORSHeaders(w.Header(), r, h.admission)
	if isDataPlaneRoute(r.URL.Path) && r.Method != http.MethodOptions {
		*r = *bindHarnessIdentity(r)
	}
	if !isDataPlaneRoute(r.URL.Path) || r.Method != http.MethodOptions {
		return false
	}
	if !h.admission.allowsRequestOrigin(requestViewFromHTTP(r)) {
		w.WriteHeader(http.StatusForbidden)
		return true
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}
