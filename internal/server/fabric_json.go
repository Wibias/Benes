package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var (
	errFabricBodyTooLarge       = errors.New("fabric request body is too large")
	errFabricTrailingJSON       = errors.New("fabric request body has trailing JSON")
	errFabricCancelPrecondition = errors.New("fabric cancel preconditions are incomplete")
)

func decodeExactJSON(r io.Reader, limit int64, dest any) error {
	if limit <= 0 {
		limit = 1 << 16
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return errFabricBodyTooLarge
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return io.EOF
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return errFabricTrailingJSON
	}
	return nil
}

func fabricJSONError(err error) (status int, code, message string, ok bool) {
	if err == nil {
		return 0, "", "", false
	}
	if errors.Is(err, io.EOF) {
		return 400, "invalid_body", "invalid JSON body", true
	}
	if errors.Is(err, errFabricBodyTooLarge) {
		return 400, "invalid_body", "request body is too large", true
	}
	if errors.Is(err, errFabricTrailingJSON) {
		return 400, "invalid_body", "request body must contain exactly one JSON value", true
	}
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typeErr) || isJSONUnknownField(err) {
		return 400, "invalid_body", "invalid JSON body", true
	}
	return 400, "invalid_body", "invalid JSON body", true
}

func isJSONUnknownField(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return len(msg) >= 19 && (containsJSONUnknown(msg))
}

func containsJSONUnknown(msg string) bool {
	return bytes.Contains([]byte(msg), []byte("unknown field"))
}
