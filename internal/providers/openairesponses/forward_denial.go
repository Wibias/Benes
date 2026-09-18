package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/transport"
)

const (
	forwardDenialBodyMaxBytes = 64 * 1024
	forwardDenialBodyTimeout  = 5 * time.Second
)

type ForwardDenial string

const (
	ForwardDenialWorkspace   ForwardDenial = "workspace"
	ForwardDenialEntitlement ForwardDenial = "entitlement"
)

func classifyForward403Denial(ctx context.Context, body io.ReadCloser) ForwardDenial {
	if body == nil {
		return ""
	}
	readCtx, cancel := context.WithTimeout(ctx, forwardDenialBodyTimeout)
	defer cancel()

	var retained bytes.Buffer
	_, err := transport.CopyBounded(readCtx, &retained, body, transport.StreamLimits{
		MaxBytes:          forwardDenialBodyMaxBytes,
		InactivityTimeout: forwardDenialBodyTimeout,
	})
	if err != nil {
		return ""
	}
	raw := retained.Bytes()
	if len(raw) == 0 || !utf8.Valid(raw) || !json.Valid(raw) || hasDuplicateJSONKeys(raw) {
		return ""
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return ""
	}
	if code, isString := ownJSONString(root, "code"); isString {
		return classifyForwardDenialCode(code)
	}
	if denial := classifyNestedDenialCode(root, "error"); denial != "" {
		return denial
	}
	return classifyNestedDenialCode(root, "detail")
}

func classifyNestedDenialCode(root map[string]json.RawMessage, key string) ForwardDenial {
	raw, ok := root[key]
	if !ok {
		return ""
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(raw, &nested) != nil || nested == nil {
		return ""
	}
	code, isString := ownJSONString(nested, "code")
	if !isString {
		return ""
	}
	return classifyForwardDenialCode(code)
}

func classifyForwardDenialCode(code string) ForwardDenial {
	switch code {
	case "codex_workspace_access_denied", "workspace_access_denied", "invalid_workspace_selected":
		return ForwardDenialWorkspace
	case "codex_entitlement_missing", "entitlement_missing":
		return ForwardDenialEntitlement
	default:
		return ""
	}
}

func ownJSONString(object map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := object[key]
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func hasDuplicateJSONKeys(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	duplicate, err := scanJSONValueForDuplicateKeys(decoder)
	if err != nil || duplicate {
		return true
	}
	_, err = decoder.Token()
	return err != io.EOF
}

func scanJSONValueForDuplicateKeys(decoder *json.Decoder) (bool, error) {
	token, err := decoder.Token()
	if err != nil {
		return false, err
	}
	delim, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return false, nil
	}

	switch delim {
	case '{':
		keys := map[string]struct{}{}
		duplicate := false
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return false, io.ErrUnexpectedEOF
			}
			if _, exists := keys[key]; exists {
				duplicate = true
			}
			keys[key] = struct{}{}
			nestedDuplicate, err := scanJSONValueForDuplicateKeys(decoder)
			if err != nil {
				return false, err
			}
			duplicate = duplicate || nestedDuplicate
		}
		end, err := decoder.Token()
		if err != nil {
			return false, err
		}
		if end != json.Delim('}') {
			return false, io.ErrUnexpectedEOF
		}
		return duplicate, nil
	case '[':
		duplicate := false
		for decoder.More() {
			nestedDuplicate, err := scanJSONValueForDuplicateKeys(decoder)
			if err != nil {
				return false, err
			}
			duplicate = duplicate || nestedDuplicate
		}
		end, err := decoder.Token()
		if err != nil {
			return false, err
		}
		if end != json.Delim(']') {
			return false, io.ErrUnexpectedEOF
		}
		return duplicate, nil
	default:
		return false, io.ErrUnexpectedEOF
	}
}
