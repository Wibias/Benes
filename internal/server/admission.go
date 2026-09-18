package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/url"
	"strings"
)

type DataPlaneAdmissionPolicy struct {
	BindHostname      string
	DataPlaneTokens   []string
	CORSAllowOrigins  []string
	CORSDefaultOrigin string
}

type admissionKind uint8

const (
	admissionLegacyBearer admissionKind = iota + 1
	admissionLoopback
	admissionRemote
)

type dataPlaneAdmission struct {
	kind              admissionKind
	tokenHashes       [][sha256.Size]byte
	corsAllowOrigins  []string
	corsDefaultOrigin string
}

func buildDataPlaneAdmission(options Options) (dataPlaneAdmission, error) {
	if options.AdmissionPolicy != nil {
		if options.DataPlaneToken != "" || len(options.DataPlaneTokens) > 0 {
			return dataPlaneAdmission{}, fmt.Errorf("explicit admission policy cannot be combined with legacy data-plane token fields")
		}
		host := normalizeHostname(options.AdmissionPolicy.BindHostname)
		if host == "" {
			return dataPlaneAdmission{}, fmt.Errorf("explicit admission policy requires a bind hostname")
		}
		corsAllowOrigins := append([]string(nil), options.AdmissionPolicy.CORSAllowOrigins...)
		corsDefaultOrigin, err := normalizeCORSDefaultOrigin(options.AdmissionPolicy.CORSDefaultOrigin)
		if err != nil {
			return dataPlaneAdmission{}, err
		}
		if isLoopbackHostname(host) {
			return dataPlaneAdmission{
				kind:              admissionLoopback,
				corsAllowOrigins:  corsAllowOrigins,
				corsDefaultOrigin: corsDefaultOrigin,
			}, nil
		}
		hashes, err := hashAdmissionTokens(options.AdmissionPolicy.DataPlaneTokens)
		if err != nil {
			return dataPlaneAdmission{}, fmt.Errorf("non-loopback listener: %w", err)
		}
		return dataPlaneAdmission{
			kind:              admissionRemote,
			tokenHashes:       hashes,
			corsAllowOrigins:  corsAllowOrigins,
			corsDefaultOrigin: corsDefaultOrigin,
		}, nil
	}

	tokens := options.DataPlaneTokens
	if len(tokens) == 0 && options.DataPlaneToken != "" {
		tokens = []string{options.DataPlaneToken}
	}
	hashes, err := hashAdmissionTokens(tokens)
	if err != nil {
		return dataPlaneAdmission{}, fmt.Errorf("legacy bearer admission: %w", err)
	}
	return dataPlaneAdmission{
		kind:              admissionLegacyBearer,
		tokenHashes:       hashes,
		corsDefaultOrigin: defaultCORSOrigin,
	}, nil
}

func (a dataPlaneAdmission) admit(r requestView) (status int, allowed bool) {
	switch a.kind {
	case admissionLegacyBearer:
		if tokenHeaderMatches(r.authorization, "Bearer ", a.tokenHashes) {
			return 0, true
		}
		return 401, false

	case admissionLoopback:
		if !isLoopbackRequestHost(r.host) || !originAllowed(r, true, a.corsAllowOrigins) {
			return 403, false
		}
		return 0, true

	case admissionRemote:
		if !tokenMatches(r.dedicatedKey, a.tokenHashes) {
			return 401, false
		}
		if !originAllowed(r, false, a.corsAllowOrigins) {
			return 403, false
		}
		return 0, true

	default:
		return 401, false
	}
}

type requestView struct {
	host          string
	origin        string
	authorization string
	dedicatedKey  string
	tls           bool
}

func normalizeHostname(hostname string) string {
	normalized := strings.ToLower(strings.TrimSpace(hostname))
	normalized = strings.TrimSuffix(normalized, ".")
	if strings.HasPrefix(normalized, "[") && strings.HasSuffix(normalized, "]") {
		normalized = normalized[1 : len(normalized)-1]
	}
	return normalized
}

func isLoopbackHostname(hostname string) bool {
	switch normalizeHostname(hostname) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func isLoopbackRequestHost(hostport string) bool {
	parsed, err := url.Parse("http://" + strings.TrimSpace(hostport))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return isLoopbackHostname(parsed.Hostname())
}

func originAllowed(r requestView, loopbackListener bool, corsAllowOrigins []string) bool {
	origin := strings.TrimSpace(r.origin)
	if origin == "" {
		return true
	}
	if extraOriginAllowed(origin, corsAllowOrigins) {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if isLoopbackHostname(parsed.Hostname()) {
		return true
	}
	if loopbackListener {
		return false
	}
	requestScheme := "http"
	if r.tls {
		requestScheme = "https"
	}
	return sameOrigin(parsed, requestScheme, r.host)
}

func extraOriginAllowed(origin string, corsAllowOrigins []string) bool {
	originComparable, originOK := comparableOrigin(origin)
	for _, allowed := range corsAllowOrigins {
		comparableAllowed, allowedOK := comparableOrigin(allowed)
		if originOK && allowedOK {
			if originComparable == comparableAllowed {
				return true
			}
			continue
		}
		if allowed == origin {
			return true
		}
	}
	return false
}

func comparableOrigin(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if scheme == "http" || scheme == "https" {
		if port == effectiveOriginPort(scheme, "") {
			port = ""
		}
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, true
}

func sameOrigin(origin *url.URL, requestScheme, requestHost string) bool {
	requestURL, err := url.Parse(requestScheme + "://" + strings.TrimSpace(requestHost))
	if err != nil || requestURL.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(origin.Scheme, requestScheme) {
		return false
	}
	if !strings.EqualFold(origin.Hostname(), requestURL.Hostname()) {
		return false
	}
	return effectiveOriginPort(origin.Scheme, origin.Port()) == effectiveOriginPort(requestScheme, requestURL.Port())
}

func effectiveOriginPort(scheme, port string) string {
	if port != "" {
		return port
	}
	switch strings.ToLower(scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func hashAdmissionTokens(tokens []string) ([][sha256.Size]byte, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("at least one data-plane credential is required")
	}
	seen := make(map[[sha256.Size]byte]struct{}, len(tokens))
	hashes := make([][sha256.Size]byte, 0, len(tokens))
	for _, token := range tokens {
		if token == "" || token != strings.TrimSpace(token) {
			return nil, fmt.Errorf("data-plane credentials must be non-empty and contain no surrounding whitespace")
		}
		hash := sha256.Sum256([]byte(token))
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}
	return hashes, nil
}

func tokenHeaderMatches(header, prefix string, tokenHashes [][sha256.Size]byte) bool {
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	return tokenMatches(header[len(prefix):], tokenHashes)
}

func tokenMatches(candidate string, tokenHashes [][sha256.Size]byte) bool {
	if candidate == "" {
		return false
	}
	candidateHash := sha256.Sum256([]byte(candidate))
	matched := 0
	for index := range tokenHashes {
		matched |= subtle.ConstantTimeCompare(candidateHash[:], tokenHashes[index][:])
	}
	return matched == 1
}

// ValidateDataPlaneAdmissionPolicy checks listener admission before any provider
// construction or network work begins.
func ValidateDataPlaneAdmissionPolicy(policy DataPlaneAdmissionPolicy) error {
	_, err := buildDataPlaneAdmission(Options{AdmissionPolicy: &policy})
	return err
}
