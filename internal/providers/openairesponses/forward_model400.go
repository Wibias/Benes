package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/transport"
)

const (
	forwardModel400BodyMaxBytes = 64 * 1024
	forwardModel400BodyTimeout  = 5 * time.Second
)

func readForwardErrorBody(ctx context.Context, response *http.Response) []byte {
	if response == nil || response.Body == nil {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, forwardModel400BodyTimeout)
	defer cancel()

	var retained bytes.Buffer
	_, err := transport.CopyBounded(readCtx, &retained, response.Body, transport.StreamLimits{
		MaxBytes:          forwardModel400BodyMaxBytes,
		InactivityTimeout: forwardModel400BodyTimeout,
	})
	if err != nil {
		return nil
	}
	raw := retained.Bytes()
	if !utf8.Valid(raw) {
		return nil
	}
	return raw
}

func shouldRetryForwardPoolModel400(status int, body []byte, modelID string) bool {
	return isAllowListedCodexAccountModel400(status, body, modelID)
}

func isAllowListedCodexAccountModel400(status int, body []byte, modelID string) bool {
	if status != http.StatusBadRequest || !utf8.Valid(body) || strings.TrimSpace(modelID) == "" {
		return false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return false
	}
	rawDetail, ok := root["detail"]
	if !ok {
		return false
	}
	var detail string
	if err := json.Unmarshal(rawDetail, &detail); err != nil {
		return false
	}
	expected := "The '" + modelID + "' model is not supported when using Codex with a ChatGPT account."
	return normalizeCodexUnsupportedModelDetail(detail) == normalizeCodexUnsupportedModelDetail(expected)
}

func normalizeCodexUnsupportedModelDetail(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
