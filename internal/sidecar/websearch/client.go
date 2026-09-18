package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/authpublic"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/sidecar"
)

var (
	ErrUnsupportedSelection = errors.New("web search sidecar is not enabled for this provider")
	ErrMissingCredential    = errors.New("web search sidecar requires a resolvable API key")
	ErrUpstreamFailed       = errors.New("web search sidecar request failed")
)

const DefaultMaxSearches = 3

type Config struct {
	ProviderID    string
	Endpoint      string
	APIKey        string
	AuthClass     string
	Wire          string
	ModelID       string
	AllowedModels []string
	Enabled       bool
	HTTPClient    *http.Client
	MaxErrorBytes int64
	CredentialRef credentials.Ref
	MaxSearches   int
}

type Client struct {
	endpoint    string
	apiKey      string
	httpClient  *http.Client
	maxErr      int64
	maxSearches int
	wire        string
	modelID     string
	observer    SearchObserver
}

// SearchObserver is notified when the sidecar is actually invoked. It exists so
// a caller can record real sidecar execution instead of inferring it from policy
// selection, eligibility, or client construction.
type SearchObserver func()

type Result struct {
	Query   string
	Text    string
	Sources []Source
}
type Source struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func New(config Config) (*Client, error) {
	if err := ValidateSelection(config); err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, ErrMissingCredential
	}
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("web search HTTP client is required")
	}
	httpClient := *config.HTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	maxErr := config.MaxErrorBytes
	if maxErr <= 0 {
		maxErr = 4096
	}
	return &Client{endpoint: strings.TrimSpace(config.Endpoint), apiKey: config.APIKey, httpClient: &httpClient, maxErr: maxErr, maxSearches: maxSearches(config.MaxSearches), wire: strings.ToLower(strings.TrimSpace(config.Wire)), modelID: strings.TrimSpace(config.ModelID)}, nil
}

func maxSearches(n int) int {
	if n <= 0 {
		return DefaultMaxSearches
	}
	return n
}

// Observe registers the single execution observer. Register it before the client
// is handed to a dispatch path; the observer is not replaced afterwards.
func (c *Client) Observe(observer SearchObserver) {
	if c == nil {
		return
	}
	c.observer = observer
}

func (c *Client) notifySearch() {
	if c == nil || c.observer == nil {
		return
	}
	c.observer()
}

func ValidateSelection(config Config) error {
	if !config.Enabled {
		return ErrUnsupportedSelection
	}
	if !strings.EqualFold(strings.TrimSpace(config.AuthClass), "key") {
		return ErrUnsupportedSelection
	}
	wire := strings.ToLower(strings.TrimSpace(config.Wire))
	class := sidecar.ClassForWire(wire)
	if class == "" {
		return ErrUnsupportedSelection
	}
	if strings.TrimSpace(config.ProviderID) == "" || strings.TrimSpace(config.ModelID) == "" {
		return ErrUnsupportedSelection
	}
	if !modelAllowed(config.AllowedModels, config.ModelID) {
		return ErrUnsupportedSelection
	}
	parsed, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return ErrUnsupportedSelection
	}
	return nil
}

func (c *Client) Search(ctx context.Context, query string) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, fmt.Errorf("web search query is required")
	}
	// Everything below this point is the actual sidecar invocation, which is the
	// only thing evidence may treat as "the web-search sidecar ran".
	c.notifySearch()
	payloadBody := map[string]any{"query": query}
	accept := "application/json"
	if c != nil && c.wire == "anthropic-messages" {
		payloadBody = map[string]any{
			"model":      c.modelID,
			"max_tokens": 8192,
			"stream":     true,
			"messages":   []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": query}}}},
			"tools":      []any{map[string]any{"type": "web_search_20250305", "name": "web_search", "max_uses": 3}},
		}
		accept = "text/event-stream"
	}
	body, err := json.Marshal(payloadBody)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	if c != nil && c.wire == "anthropic-messages" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, authpublic.SidecarConnect()
	}
	defer response.Body.Close()
	limit := c.maxErr
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") && limit < 1<<20 {
		limit = 1 << 20
	}
	limited := io.LimitReader(response.Body, limit)
	payload, _ := io.ReadAll(limited)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, authpublic.SidecarHTTP(response.StatusCode, string(payload))
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		got, err := parseAnthropicSidecarSSE(bytes.NewReader(payload))
		if err != nil {
			return Result{}, err
		}
		got.Query = query
		got.Text = sidecar.BoundText(got.Text, 16000)
		return got, nil
	}
	var decoded struct {
		Text    string `json:"text"`
		Answer  string `json:"answer"`
		Sources []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"sources"`
	}
	if json.Unmarshal(payload, &decoded) != nil {
		return Result{}, authpublic.SidecarStream()
	}
	out := Result{Query: query, Text: strings.TrimSpace(decoded.Text)}
	if out.Text == "" {
		out.Text = strings.TrimSpace(decoded.Answer)
	}
	for _, source := range decoded.Sources {
		out.Sources = append(out.Sources, Source{URL: source.URL, Title: source.Title})
	}
	out.Sources = SanitizeSources(out.Sources)
	out.Text = sidecar.BoundText(out.Text, 16000)
	return out, nil
}

func ConfigFromSpec(providerID, endpoint, apiKey, authClass, wire, modelID string, enabled bool, allowed []string) Config {
	return Config{
		ProviderID:    providerID,
		Endpoint:      endpoint,
		APIKey:        apiKey,
		AuthClass:     authClass,
		Wire:          wire,
		ModelID:       modelID,
		AllowedModels: append([]string(nil), allowed...),
		Enabled:       enabled,
	}
}

func modelAllowed(allowed []string, modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	for _, item := range allowed {
		if strings.TrimSpace(item) == modelID {
			return true
		}
	}
	return false
}
