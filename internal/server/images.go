package server

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/authpublic"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/transport"
)

const (
	imageGenerationsPath     = "/v1/images/generations"
	imageEditsPath           = "/v1/images/edits"
	defaultImageRequestBytes = 64 << 20
	defaultImageTimeout      = 5 * time.Minute
	maxImagePartBytes        = 50 << 20
)

var (
	errImageTooLarge            = errors.New("image request is too large")
	errImageUnsupportedEncoding = errors.New("image request encoding is not supported")
	errImageUpstreamMissing     = errors.New("no eligible image upstream is configured")
	errImageUpstreamUnsupported = errors.New("selected provider cannot relay image generation")
	errImageUpstreamAmbiguous   = errors.New("image upstream is ambiguous across providers")
	errImagePrivateReference    = errors.New("image reference URL is not allowed")
	errImageUnsupportedKind     = errors.New("unsupported image relay path")
)

func (h *handler) handleImages(w http.ResponseWriter, r *http.Request, trace *timeline.Trace) {
	kind, err := imageRelayKind(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	maxBytes := h.imageRequestBytes()
	body, err := readImageBody(r, maxBytes)
	if err != nil {
		switch {
		case errors.Is(err, errImageTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image request is too large")
		case errors.Is(err, errImageUnsupportedEncoding):
			writeError(w, http.StatusUnsupportedMediaType, "invalid_request", "image request encoding is not supported")
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid image request")
		}
		return
	}
	contentType := r.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	if err := inspectImageBody(body, contentType, maxBytes); err != nil {
		if errors.Is(err, errImageTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image request is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid image request")
		return
	}
	if err := rejectPrivateImageRefs(r.Context(), body, contentType); err != nil {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "image_ref_blocked")
		writeError(w, http.StatusBadRequest, "invalid_request", "image reference URL is not allowed")
		return
	}
	model, err := imageRequestModel(body, contentType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid image request")
		return
	}
	provider, route, err := h.resolveImageProvider(model)
	if err != nil {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "image_upstream_unavailable")
		writeError(w, http.StatusBadRequest, "invalid_request", imageUpstreamMessage(err))
		return
	}
	relayer := asImageRelay(provider)
	if relayer == nil {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "image_upstream_unavailable")
		writeError(w, http.StatusBadRequest, "invalid_request", imageUpstreamMessage(errImageUpstreamUnsupported))
		return
	}
	turn, err := h.acquireTurn(r.Context(), len(body))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
		return
	}
	if turn != nil {
		defer turn.Close()
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.imageRequestTimeout())
	defer cancel()
	trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", true, "")
	dispatch := providercontract.DispatchRequest{
		ForwardHeaders:        h.snapshotForwardHeaders(r.Header),
		CodexAccountID:        route.CodexAccountID,
		Turn:                  turn,
		ConfiguredServiceTier: admittedServiceTier(ctx),
	}
	status, header, payload, relayErr := relayer.RelayImage(ctx, dispatch, providercontract.ImageRelayRequest{
		Kind:        kind,
		Body:        body,
		ContentType: contentType,
	})
	if relayErr != nil {
		writeImageTransportError(w, r.Context(), ctx, relayErr)
		return
	}
	trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, true, "")
	writeNativeCompact(w, status, header, payload)
	if turn != nil {
		turn.MarkCommitted()
	}
}

func (h *handler) imageRequestBytes() int64 {
	if h != nil && h.imageMaxRequestBytes > 0 {
		return h.imageMaxRequestBytes
	}
	return defaultImageRequestBytes
}

func (h *handler) imageRequestTimeout() time.Duration {
	if h != nil && h.imageTimeout > 0 {
		return h.imageTimeout
	}
	return defaultImageTimeout
}

func imageRelayKind(path string) (providercontract.ImageRelayKind, error) {
	switch path {
	case imageGenerationsPath:
		return providercontract.ImageRelayGenerations, nil
	case imageEditsPath:
		return providercontract.ImageRelayEdits, nil
	default:
		return "", errImageUnsupportedKind
	}
}

func asImageRelay(provider Provider) providercontract.ImageRelay {
	relayer, ok := provider.(providercontract.ImageRelay)
	if !ok || relayer == nil || !relayer.SupportsImageRelay() {
		return nil
	}
	return relayer
}

func (h *handler) resolveImageProvider(model string) (Provider, router.Route, error) {
	model = strings.TrimSpace(model)
	if route, err := h.parseRoute(model); err == nil && strings.TrimSpace(route.Provider) != "" {
		resolved := h.resolveProvider(route)
		if resolved.MissingCombo || resolved.MissingProvider {
			return nil, route, errImageUpstreamMissing
		}
		if asImageRelay(resolved.Provider) == nil {
			return nil, route, errImageUpstreamUnsupported
		}
		return resolved.Provider, route, nil
	}
	var found Provider
	var foundID string
	for id, provider := range h.providers {
		wrapped := withProviderRouteAttribution(provider, id, id, model)
		if asImageRelay(wrapped) == nil {
			continue
		}
		if found != nil {
			return nil, router.Route{}, errImageUpstreamAmbiguous
		}
		found = wrapped
		foundID = id
	}
	if found == nil {
		return nil, router.Route{}, errImageUpstreamMissing
	}
	return found, router.Route{Provider: foundID, Model: model}, nil
}

func imageUpstreamMessage(err error) string {
	switch {
	case errors.Is(err, errImageUpstreamUnsupported):
		return "selected provider cannot relay Codex image generation; configure an OpenAI image-capable upstream or disable image_generation"
	case errors.Is(err, errImageUpstreamAmbiguous):
		return "image generation needs an explicit provider/model because multiple image-capable upstreams are configured"
	default:
		return "built-in image generation needs an OpenAI image-capable upstream (ChatGPT login or an OpenAI API-key provider); add one or disable the tool with `codex features disable image_generation`"
	}
}

func readImageBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, fmt.Errorf("missing image request body")
	}
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, maxBytes+1)
	reader := io.Reader(limited)
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	switch encoding {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(limited)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = io.LimitReader(gz, maxBytes+1)
	case "deflate":
		fl := flate.NewReader(limited)
		defer fl.Close()
		reader = io.LimitReader(fl, maxBytes+1)
	default:
		return nil, errImageUnsupportedEncoding
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errImageTooLarge
	}
	return body, nil
}

func inspectImageBody(body []byte, contentType string, maxBytes int64) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return fmt.Errorf("image request must be a JSON object")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(trimmed, &fields) != nil {
			return fmt.Errorf("image request must be a JSON object")
		}
		return nil
	}
	boundary := params["boundary"]
	if boundary == "" {
		return fmt.Errorf("multipart image request is missing a boundary")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		limited := io.LimitReader(part, maxImagePartBytes+1)
		payload, readErr := io.ReadAll(limited)
		_ = part.Close()
		if readErr != nil {
			return readErr
		}
		if int64(len(payload)) > maxImagePartBytes || int64(len(payload)) > maxBytes {
			return errImageTooLarge
		}
	}
}

func imageRequestModel(body []byte, contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", fmt.Errorf("multipart image request is missing a boundary")
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			name := part.FormName()
			payload, readErr := io.ReadAll(io.LimitReader(part, 1<<20))
			_ = part.Close()
			if readErr != nil {
				return "", readErr
			}
			if name == "model" {
				return strings.TrimSpace(string(payload)), nil
			}
		}
	}
	var envelope struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return "", fmt.Errorf("image request must be a JSON object")
	}
	return strings.TrimSpace(envelope.Model), nil
}

func rejectPrivateImageRefs(ctx context.Context, body []byte, contentType string) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return nil
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return nil
	}
	return walkImageRefs(ctx, value, "")
}

func walkImageRefs(ctx context.Context, value any, key string) error {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			if err := walkImageRefs(ctx, child, childKey); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := walkImageRefs(ctx, child, key); err != nil {
				return err
			}
		}
	case string:
		if !imageRefKey(key) || !looksLikeRemoteURL(typed) {
			return nil
		}
		return validateImageRefURL(ctx, typed)
	}
	return nil
}

func imageRefKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "image_url", "url", "image":
		return true
	default:
		return false
	}
}

func looksLikeRemoteURL(raw string) bool {
	value := strings.TrimSpace(raw)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func validateImageRefURL(ctx context.Context, raw string) error {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errImagePrivateReference
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errImagePrivateReference
	}
	if _, err := transport.ResolveTarget(ctx, value, transport.DestinationPolicy{}); err != nil {
		return errImagePrivateReference
	}
	return nil
}

func writeImageTransportError(w http.ResponseWriter, parent, ctx context.Context, err error) {
	if parent != nil && parent.Err() != nil {
		writeError(w, 499, "client_cancelled", "client cancelled image request")
		return
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "upstream_error", "image upstream timed out")
		return
	}
	if errors.Is(err, context.Canceled) {
		writeError(w, 499, "client_cancelled", "client cancelled image request")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "upstream_error", "image upstream timed out")
		return
	}
	if authpublic.IsAuthentication(err) {
		writeError(w, http.StatusUnauthorized, "authentication_error", authpublic.Project(err))
		return
	}
	writeError(w, http.StatusBadGateway, "upstream_error", "image upstream request failed")
}
