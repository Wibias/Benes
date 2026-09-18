package modelprobe

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
	"time"

	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/transport"
)

const (
	maxProbeBodyBytes = 1 << 20
	defaultTimeout    = 8 * time.Second
	generationPrompt  = "."
)

type Request struct {
	Provider       string
	Model          string
	BaseURL        string
	APIKey         string
	CredentialSlot string
	AllowPrivate   bool
	Generate       bool
	Adapter        string
	AuthMode       string
	LiveModels     *bool
	Timeout        time.Duration
	Transport      http.RoundTripper
	Now            func() time.Time
}

func Probe(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	provider := strings.TrimSpace(req.Provider)
	model := strings.TrimSpace(req.Model)
	result := UnknownResult(provider, model)
	result.CredentialSlot = strings.TrimSpace(req.CredentialSlot)
	now := time.Now().UTC()
	if req.Now != nil {
		now = req.Now().UTC()
	}
	if provider == "" || model == "" {
		result.State = StateUnsupportedProbe
		result.Reason = "invalid_config"
		result.TestedAt = now
		return result.Safe(), nil
	}
	if strings.EqualFold(strings.TrimSpace(req.AuthMode), "disabled") {
		result.State = StateUnsupportedProbe
		result.Reason = "invalid_config"
		result.TestedAt = now
		return result.Safe(), nil
	}
	if strings.EqualFold(strings.TrimSpace(req.AuthMode), "forward") && !req.Generate {
		result.State = StateUnsupportedProbe
		result.Reason = "forward_auth"
		result.TestedAt = now
		return result.Safe(), nil
	}
	if req.LiveModels != nil && !*req.LiveModels && !req.Generate {
		result.State = StateUnsupportedProbe
		result.Reason = "static_catalog"
		result.TestedAt = now
		return result.Safe(), nil
	}
	base := strings.TrimSpace(req.BaseURL)
	if base == "" {
		result.State = StateUnsupportedProbe
		result.Reason = "missing_destination"
		result.TestedAt = now
		return result.Safe(), nil
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" {
		result.State = StateUnsupportedProbe
		result.Reason = "missing_destination"
		result.TestedAt = now
		return result.Safe(), nil
	}
	if parsed.User != nil {
		result.State = StateUnsupportedProbe
		result.Reason = "lookalike_destination"
		result.TestedAt = now
		return result.Safe(), nil
	}
	result.DestinationHost = strings.ToLower(parsed.Hostname())
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := req.Transport
	if client == nil {
		target, err := transport.ResolveTarget(probeCtx, modelsURL(base), transport.DestinationPolicy{
			AllowPrivateNetwork: req.AllowPrivate,
		})
		if err != nil {
			result.State = StateUnavailable
			result.Reason = "private_destination"
			result.TestedAt = now
			if probeCtx.Err() != nil {
				if ctx.Err() != nil {
					return Result{}, ctx.Err()
				}
				result.Reason = "timeout"
			}
			return result.Safe(), nil
		}
		client = transport.NewClient(target, transport.ClientOptions{ResponseHeaderTimeout: timeout}).Transport
	}
	observed := transport.Observe(client)
	httpClient := &http.Client{Transport: observed, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	if req.Generate {
		return probeGeneration(probeCtx, httpClient, req, result, now)
	}
	return probeCatalog(probeCtx, httpClient, req, result, now)
}

func probeCatalog(ctx context.Context, client *http.Client, req Request, result Result, now time.Time) (Result, error) {
	result.Kind = KindCatalog
	result.GenerationIncurred = false
	trace := timeline.New("model-probe-catalog", 8)
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(timeline.WithTrace(ctx, trace), http.MethodGet, modelsURL(req.BaseURL), nil)
	if err != nil {
		result.State = StateUnavailable
		result.Reason = "invalid_config"
		result.TestedAt = now
		return result.Safe(), nil
	}
	if key := strings.TrimSpace(req.APIKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(httpReq)
	result.Timing = timingFrom(trace, start)
	result.TestedAt = now
	if err != nil {
		return finishTransportError(ctx, result, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxProbeBodyBytes))
	state := ClassifyStatus(res.StatusCode)
	result.State = state
	result.Reason = reasonForStatus(res.StatusCode)
	if state != StateAvailable {
		return result.Safe(), nil
	}
	if !catalogListsModel(body, req.Model) {
		result.State = StateUnknown
		result.Reason = "model_not_listed"
	}
	return result.Safe(), nil
}

func probeGeneration(ctx context.Context, client *http.Client, req Request, result Result, now time.Time) (Result, error) {
	result.Kind = KindGeneration
	result.GenerationIncurred = true
	payload, err := json.Marshal(map[string]any{
		"model":      strings.TrimSpace(req.Model),
		"messages":   []map[string]string{{"role": "user", "content": generationPrompt}},
		"max_tokens": 1,
		"stream":     false,
	})
	if err != nil {
		result.State = StateUnavailable
		result.Reason = "invalid_config"
		result.TestedAt = now
		result.GenerationIncurred = false
		return result.Safe(), nil
	}
	trace := timeline.New("model-probe-generation", 8)
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(timeline.WithTrace(ctx, trace), http.MethodPost, chatURL(req.BaseURL), bytes.NewReader(payload))
	if err != nil {
		result.State = StateUnavailable
		result.Reason = "invalid_config"
		result.TestedAt = now
		result.GenerationIncurred = false
		return result.Safe(), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(req.APIKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(httpReq)
	result.Timing = timingFrom(trace, start)
	result.TestedAt = now
	if err != nil {
		return finishTransportError(ctx, result, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxProbeBodyBytes))
	result.State = ClassifyStatus(res.StatusCode)
	result.Reason = reasonForStatus(res.StatusCode)
	return result.Safe(), nil
}

func finishTransportError(ctx context.Context, result Result, err error) (Result, error) {
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return Result{}, ctx.Err()
		}
		result.State = StateUnavailable
		result.Reason = "timeout"
		return result.Safe(), nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.State = StateUnavailable
		result.Reason = "timeout"
		return result.Safe(), nil
	}
	result.State = StateUnavailable
	result.Reason = "network"
	return result.Safe(), nil
}

func timingFrom(tr *timeline.Trace, start time.Time) Timing {
	total := time.Since(start).Milliseconds()
	if total < 0 {
		total = 0
	}
	out := Timing{TotalMs: total}
	for _, event := range tr.Events() {
		ms := event.Elapsed.Milliseconds()
		switch event.Milestone {
		case timeline.MilestoneHeaders:
			out.HeadersMs = int64Ptr(ms)
		case timeline.MilestoneFirstByte, timeline.MilestoneTTFT:
			out.FirstOutputMs = int64Ptr(ms)
			out.TTFTMs = int64Ptr(ms)
		}
	}
	return out
}

func modelsURL(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	trimmed = strings.TrimSuffix(trimmed, "/chat/completions")
	trimmed = strings.TrimSuffix(trimmed, "/responses")
	trimmed = strings.TrimRight(trimmed, "/")
	if strings.HasSuffix(trimmed, "/models") {
		return trimmed
	}
	return trimmed + "/models"
}

func chatURL(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return trimmed
	}
	trimmed = strings.TrimSuffix(trimmed, "/responses")
	trimmed = strings.TrimSuffix(trimmed, "/models")
	trimmed = strings.TrimRight(trimmed, "/")
	return trimmed + "/chat/completions"
}

func catalogListsModel(body []byte, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	for _, row := range payload.Data {
		if strings.TrimSpace(row.ID) == model {
			return true
		}
	}
	for _, row := range payload.Models {
		if strings.TrimSpace(row.ID) == model {
			return true
		}
	}
	return false
}

func reasonForStatus(status int) string {
	switch {
	case status == 401:
		return "http_401"
	case status == 403:
		return "http_403"
	case status == 402:
		return "http_402"
	case status == 429:
		return "http_429"
	case status == 404:
		return "http_404"
	case status >= 500:
		return "http_5xx"
	case status >= 200 && status < 300:
		return ""
	default:
		return fmt.Sprintf("http_%d", status)
	}
}

func int64Ptr(v int64) *int64 { return &v }
