package google

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/transport"
)

const (
	oauthTokenURL      = "https://oauth2.googleapis.com/token"
	metadataTokenURL   = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	jwtBearerGrant     = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	adcTimeout         = 15 * time.Second
	metadataTimeout    = 2 * time.Second
	adcSkew            = 60 * time.Second
	tokenAttempts      = 3
	tokenRetryBase     = 300 * time.Millisecond
)

var (
	ErrMissingADC = fmt.Errorf("Vertex AI requires Application Default Credentials")
)

type ADCEnv struct {
	Environ  map[string]string
	Home     string
	ReadFile func(string) ([]byte, error)
	Stat     func(string) (fs.FileInfo, error)
	HTTP     *http.Client
	Now      func() time.Time
	Sleep    func(time.Duration)
	Metadata string
}

type adcCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
	source  string
}

var globalADC adcCache

func (e ADCEnv) getenv(key string) string {
	if e.Environ != nil {
		return strings.TrimSpace(e.Environ[key])
	}
	return strings.TrimSpace(os.Getenv(key))
}

func (e ADCEnv) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e ADCEnv) readFile(path string) ([]byte, error) {
	if e.ReadFile != nil {
		return e.ReadFile(path)
	}
	return os.ReadFile(path)
}

func (e ADCEnv) stat(path string) (fs.FileInfo, error) {
	if e.Stat != nil {
		return e.Stat(path)
	}
	return os.Stat(path)
}

func (e ADCEnv) httpClient() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return transport.DefaultUnpinnedClient()
}

func (e ADCEnv) sleep(d time.Duration) {
	if e.Sleep != nil {
		e.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (e ADCEnv) metadataURL() string {
	if u := strings.TrimSpace(e.Metadata); u != "" {
		return u
	}
	return metadataTokenURL
}

func userADCPath(e ADCEnv) string {
	if cfg := e.getenv("CLOUDSDK_CONFIG"); cfg != "" {
		return filepath.Join(cfg, "application_default_credentials.json")
	}
	if runtime.GOOS == "windows" {
		if appdata := e.getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "gcloud", "application_default_credentials.json")
		}
	}
	home := e.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
}

func fileSourceTag(prefix, path string, env ADCEnv) string {
	info, err := env.stat(path)
	if err != nil {
		return prefix + ":" + path
	}
	return fmt.Sprintf("%s:%s:%d:%d", prefix, path, info.Size(), info.ModTime().UnixMilli())
}

func AccessToken(ctx context.Context, env ADCEnv) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	source, creds, err := loadADCFile(env)
	if err != nil {
		if env.getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
			return "", err
		}
		if cached := cachedADC("metadata", env.now()); cached != "" {
			return cached, nil
		}
		tok, expires, metaErr := fetchMetadataToken(ctx, env)
		if metaErr != nil {
			return "", err
		}
		return rememberADC("metadata", tok, expires), nil
	}
	if cached := cachedADC(source, env.now()); cached != "" {
		return cached, nil
	}
	tok, expires, err := exchangeADC(ctx, env, creds)
	if err != nil {
		return "", err
	}
	return rememberADC(source, tok, expires), nil
}

func cachedADC(source string, now time.Time) string {
	globalADC.mu.Lock()
	defer globalADC.mu.Unlock()
	if globalADC.token != "" && globalADC.source == source && now.Add(adcSkew).Before(globalADC.expires) {
		return globalADC.token
	}
	return ""
}

func rememberADC(source, tok string, expires time.Time) string {
	globalADC.mu.Lock()
	globalADC.token = tok
	globalADC.expires = expires
	globalADC.source = source
	globalADC.mu.Unlock()
	return tok
}

func loadADCFile(env ADCEnv) (string, map[string]any, error) {
	path := env.getenv("GOOGLE_APPLICATION_CREDENTIALS")
	prefix := "gac"
	if path == "" {
		path = userADCPath(env)
		prefix = "user"
	}
	raw, err := env.readFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("%w: set GOOGLE_APPLICATION_CREDENTIALS, run gcloud auth application-default login, or run on a GCE/Cloud Run instance", ErrMissingADC)
	}
	var creds map[string]any
	if json.Unmarshal(raw, &creds) != nil {
		return "", nil, fmt.Errorf("Vertex ADC file is not valid JSON")
	}
	return fileSourceTag(prefix, path, env), creds, nil
}

func exchangeADC(ctx context.Context, env ADCEnv, creds map[string]any) (string, time.Time, error) {
	kind, _ := creds["type"].(string)
	switch kind {
	case "authorized_user":
		return exchangeRefresh(ctx, env, creds)
	case "service_account":
		return exchangeServiceAccount(ctx, env, creds)
	default:
		return "", time.Time{}, fmt.Errorf("Vertex ADC type %q is unsupported", kind)
	}
}

func exchangeRefresh(ctx context.Context, env ADCEnv, creds map[string]any) (string, time.Time, error) {
	id, _ := creds["client_id"].(string)
	secret, _ := creds["client_secret"].(string)
	refresh, _ := creds["refresh_token"].(string)
	if id == "" || secret == "" || refresh == "" {
		return "", time.Time{}, fmt.Errorf("Vertex authorized_user credentials are incomplete")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {id},
		"client_secret": {secret},
		"refresh_token": {refresh},
	}
	return postToken(ctx, env, form)
}

func exchangeServiceAccount(ctx context.Context, env ADCEnv, creds map[string]any) (string, time.Time, error) {
	email, _ := creds["client_email"].(string)
	pemKey, _ := creds["private_key"].(string)
	if email == "" || pemKey == "" {
		return "", time.Time{}, fmt.Errorf("Vertex service_account credentials are incomplete")
	}
	now := env.now().Unix()
	claims, _ := json.Marshal(map[string]any{
		"iss":   email,
		"scope": cloudPlatformScope,
		"aud":   oauthTokenURL,
		"iat":   now,
		"exp":   now + 3600,
	})
	assertion, err := signRS256JWT(claims, pemKey, stringFieldMap(creds, "private_key_id"))
	if err != nil {
		return "", time.Time{}, err
	}
	form := url.Values{
		"grant_type": {jwtBearerGrant},
		"assertion":  {assertion},
	}
	return postToken(ctx, env, form)
}

func stringFieldMap(data map[string]any, key string) string {
	v, _ := data[key].(string)
	return v
}

func postToken(ctx context.Context, env ADCEnv, form url.Values) (string, time.Time, error) {
	var last error
	for attempt := 0; attempt < tokenAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", time.Time{}, err
		}
		tok, expires, err := postTokenOnce(ctx, env, form)
		if err == nil {
			return tok, expires, nil
		}
		last = err
		if attempt == tokenAttempts-1 || !isRetryableTokenError(err) {
			return "", time.Time{}, err
		}
		env.sleep(tokenRetryBase << attempt)
	}
	return "", time.Time{}, last
}

func isRetryableTokenError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []int{429, 500, 502, 503, 504} {
		if strings.Contains(msg, fmt.Sprintf("HTTP %d", code)) {
			return true
		}
	}
	return false
}

func postTokenOnce(ctx context.Context, env ADCEnv, form url.Values) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, adcTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := env.httpClient().Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("Vertex ADC token exchange returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("Vertex ADC token exchange returned no access_token")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}
	return payload.AccessToken, env.now().Add(time.Duration(payload.ExpiresIn) * time.Second), nil
}

func fetchMetadataToken(ctx context.Context, env ADCEnv) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.metadataURL(), nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := env.httpClient().Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("Vertex ADC metadata returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("Vertex ADC metadata returned no access_token")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}
	return payload.AccessToken, env.now().Add(time.Duration(payload.ExpiresIn) * time.Second), nil
}

func signRS256JWT(payload []byte, pemKey, kid string) (string, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return "", fmt.Errorf("Vertex service_account private key is not PEM")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("Vertex service_account private key is not RSA")
		}
	} else if parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = parsed
	} else {
		return "", fmt.Errorf("Vertex service_account private key could not be parsed")
	}
	headerMap := map[string]any{"alg": "RS256", "typ": "JWT"}
	if kid != "" {
		headerMap["kid"] = kid
	}
	header, _ := json.Marshal(headerMap)
	signing := b64URL(header) + "." + b64URL(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + b64URL(sig), nil
}

func b64URL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}
