package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/transport"
)

const (
	refreshTimeout  = 30 * time.Second
	maxRefreshBody  = 64 << 10
	desktopRefresh  = "https://prod.%s.auth.desktop.kiro.dev/refreshToken"
	oidcRefresh     = "https://oidc.%s.amazonaws.com/token"
	desktopAuthHost = ".auth.desktop.kiro.dev"
	oidcHostSuffix  = ".amazonaws.com"
)

var (
	ErrNoRefreshToken = fmt.Errorf("Kiro: no refresh token available (re-run `kiro-cli login`)")
	regionPattern     = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+\-\d$`)
	terminalOAuth     = map[string]struct{}{
		"invalid_grant":         {},
		"refresh_token_reused":  {},
		"revoked":               {},
		"revoked_token":         {},
		"refresh_token_revoked": {},
		"access_denied":         {},
		"expired_token":         {},
	}
)

type RefreshAccount struct {
	ProfileARN   string
	SSORegion    string
	APIRegion    string
	ClientID     string
	ClientSecret string
	Source       string
}

type RefreshInput struct {
	Refresh    string
	Stored     *RefreshAccount
	Host       Host
	HTTPClient *http.Client
	Now        func() time.Time
}

type RefreshedCredential struct {
	AccessToken  string
	Refresh      string
	ExpiresUnix  int64
	ProfileARN   string
	APIRegion    string
	SSORegion    string
	ClientID     string
	ClientSecret string
}

type RefreshError struct {
	Status int
	OAuth  string
}

func (e *RefreshError) Error() string {
	if e == nil {
		return "Kiro token refresh failed"
	}
	if e.OAuth != "" {
		return fmt.Sprintf("Kiro token refresh failed: %d (%s)", e.Status, e.OAuth)
	}
	return fmt.Sprintf("Kiro token refresh failed: %d", e.Status)
}

func RefreshToken(ctx context.Context, in RefreshInput) (RefreshedCredential, error) {
	if strings.TrimSpace(in.Refresh) == "" {
		return RefreshedCredential{}, ErrNoRefreshToken
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RefreshedCredential{}, err
	}
	client := in.HTTPClient
	if client == nil {
		client = transport.DefaultUnpinnedClient()
	}
	meta := resolveRefreshAccount(in)
	out, err := refreshOnce(ctx, client, in.Refresh, meta, in.nowMS())
	if err == nil {
		return attachRouting(out, meta), nil
	}
	var refreshErr *RefreshError
	if !errorsIsRefresh400(err, &refreshErr) || in.Stored == nil {
		return RefreshedCredential{}, err
	}
	rotated, ok := matchingRotatedLocal(in)
	if !ok {
		return RefreshedCredential{}, err
	}
	retryMeta := metadataForRotated(meta, rotated)
	out, err = refreshOnce(ctx, client, rotated.Refresh, retryMeta, in.nowMS())
	if err != nil {
		return RefreshedCredential{}, err
	}
	out.Refresh = rotated.Refresh
	return attachRouting(out, retryMeta), nil
}

func (in RefreshInput) nowMS() int64 {
	if in.Now != nil {
		return in.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

func resolveRefreshAccount(in RefreshInput) RefreshAccount {
	if in.Stored != nil {
		if in.Stored.hasRouting() || in.Stored.ClientID != "" {
			return *in.Stored
		}
		if in.Stored.Source == "environment" || in.Stored.Source == "manual" {
			if env, ok := environmentRouting(in.Host); ok {
				env.Source = in.Stored.Source
				return env
			}
		}
		return *in.Stored
	}
	if env, ok := environmentRouting(in.Host); ok {
		return env
	}
	if local, err := ImportLocalSession(in.Host); err == nil && local.Credential.Refresh == in.Refresh {
		return accountFromImported(local.Credential)
	}
	return RefreshAccount{}
}

func (a RefreshAccount) hasRouting() bool {
	return strings.TrimSpace(a.ProfileARN) != "" || strings.TrimSpace(a.SSORegion) != "" || strings.TrimSpace(a.APIRegion) != ""
}

func environmentRouting(h Host) (RefreshAccount, bool) {
	out := RefreshAccount{
		ProfileARN: h.getenv("KIRO_PROFILE_ARN"),
	}
	if raw := h.getenv("KIRO_API_REGION"); raw != "" {
		if region, err := RequireRegion(raw); err == nil {
			out.APIRegion = region
		}
	}
	if raw := h.getenv("KIRO_REGION"); raw != "" {
		if region, err := RequireRegion(raw); err == nil {
			out.SSORegion = region
		}
	}
	if !out.hasRouting() {
		return RefreshAccount{}, false
	}
	return out, true
}

func accountFromImported(cred ImportedCredential) RefreshAccount {
	return RefreshAccount{
		ProfileARN:   cred.ProfileARN,
		SSORegion:    cred.SSORegion,
		APIRegion:    cred.APIRegion,
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		Source:       "local-cli",
	}
}

func refreshOnce(ctx context.Context, client *http.Client, refresh string, meta RefreshAccount, nowMS int64) (RefreshedCredential, error) {
	region := refreshRegion(meta)
	var (
		rawURL string
		body   []byte
	)
	if meta.ClientID != "" && meta.ClientSecret != "" {
		rawURL = fmt.Sprintf(oidcRefresh, region)
		payload, err := json.Marshal(map[string]string{
			"grantType":    "refresh_token",
			"clientId":     meta.ClientID,
			"clientSecret": meta.ClientSecret,
			"refreshToken": refresh,
		})
		if err != nil {
			return RefreshedCredential{}, err
		}
		body = payload
	} else {
		rawURL = fmt.Sprintf(desktopRefresh, region)
		payload, err := json.Marshal(map[string]string{"refreshToken": refresh})
		if err != nil {
			return RefreshedCredential{}, err
		}
		body = payload
	}
	if err := validateRefreshURL(rawURL); err != nil {
		return RefreshedCredential{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return RefreshedCredential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return RefreshedCredential{}, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxRefreshBody+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return RefreshedCredential{}, err
	}
	if len(raw) > maxRefreshBody {
		return RefreshedCredential{}, fmt.Errorf("Kiro refresh response exceeds byte cap")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RefreshedCredential{}, decodeRefreshError(resp.StatusCode, raw)
	}
	var payload struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.AccessToken == "" {
		return RefreshedCredential{}, fmt.Errorf("Kiro refresh returned no accessToken")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}
	outRefresh := payload.RefreshToken
	if outRefresh == "" {
		outRefresh = refresh
	}
	return RefreshedCredential{
		AccessToken: payload.AccessToken,
		Refresh:     outRefresh,
		ExpiresUnix: nowMS + payload.ExpiresIn*1000,
	}, nil
}

func decodeRefreshError(status int, raw []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	out := &RefreshError{Status: status}
	if json.Unmarshal(raw, &payload) == nil {
		if _, ok := terminalOAuth[payload.Error]; ok {
			out.OAuth = payload.Error
		}
	}
	return out
}

func refreshRegion(meta RefreshAccount) string {
	if region := validRegion(meta.SSORegion); region != "" {
		return region
	}
	if region := InferRegionFromProfileARN(meta.ProfileARN); validRegion(region) != "" {
		return region
	}
	if region := validRegion(meta.APIRegion); region != "" {
		return region
	}
	return DefaultRegion
}

func validRegion(raw string) string {
	normalized := NormalizeRegion(raw)
	if regionPattern.MatchString(normalized) {
		return normalized
	}
	return ""
}

func RequireRegion(raw string) (string, error) {
	if region := validRegion(raw); region != "" {
		return region, nil
	}
	return "", fmt.Errorf("Kiro: invalid region value")
}

func validateRefreshURL(raw string) error {
	if !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("Kiro refresh destination is not HTTPS")
	}
	host := raw[len("https://"):]
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if strings.HasSuffix(host, desktopAuthHost) || strings.HasSuffix(host, oidcHostSuffix) && strings.HasPrefix(host, "oidc.") {
		return nil
	}
	return fmt.Errorf("Kiro refresh destination is not a validated auth origin")
}

func matchingRotatedLocal(in RefreshInput) (ImportedCredential, bool) {
	if in.Stored == nil {
		return ImportedCredential{}, false
	}
	identity := strings.TrimSpace(in.Stored.ProfileARN)
	if identity == "" {
		return ImportedCredential{}, false
	}
	local, err := ImportLocalSession(in.Host)
	if err != nil {
		return ImportedCredential{}, false
	}
	if local.Credential.ProfileARN != identity || local.Credential.Refresh == "" || local.Credential.Refresh == in.Refresh {
		return ImportedCredential{}, false
	}
	return local.Credential, true
}

func metadataForRotated(stored RefreshAccount, local ImportedCredential) RefreshAccount {
	out := stored
	if local.SSORegion != "" {
		out.SSORegion = local.SSORegion
	}
	if local.APIRegion != "" {
		out.APIRegion = local.APIRegion
	}
	if local.ProfileARN != "" {
		out.ProfileARN = local.ProfileARN
	}
	out.ClientID = ""
	out.ClientSecret = ""
	if local.AuthType == AuthAWSSsoOIDC && local.ClientID != "" && local.ClientSecret != "" {
		out.ClientID = local.ClientID
		out.ClientSecret = local.ClientSecret
	}
	return out
}

func attachRouting(out RefreshedCredential, meta RefreshAccount) RefreshedCredential {
	out.ProfileARN = meta.ProfileARN
	out.APIRegion = meta.APIRegion
	out.SSORegion = meta.SSORegion
	out.ClientID = meta.ClientID
	out.ClientSecret = meta.ClientSecret
	return out
}

func errorsIsRefresh400(err error, dest **RefreshError) bool {
	var refreshErr *RefreshError
	if !asRefreshError(err, &refreshErr) || refreshErr.Status != http.StatusBadRequest {
		return false
	}
	*dest = refreshErr
	return true
}

func asRefreshError(err error, dest **RefreshError) bool {
	if err == nil {
		return false
	}
	typed, ok := err.(*RefreshError)
	if !ok {
		return false
	}
	*dest = typed
	return true
}
