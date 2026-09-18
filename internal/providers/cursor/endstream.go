package cursor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ParseConnectEndStream(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	var root struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &root) != nil {
		return fmt.Errorf("Cursor Connect end-stream error")
	}
	if root.Error == nil {
		return nil
	}
	code := strings.TrimSpace(root.Error.Code)
	if code == "" {
		code = "unknown"
	}
	if strings.EqualFold(code, "invalid_argument") || strings.Contains(strings.ToLower(root.Error.Message), "invalid_argument") {
		return fmt.Errorf("%w: %s", ErrInvalidArgument, code)
	}
	return fmt.Errorf("Cursor Connect error %s", code)
}

var ErrInvalidArgument = fmt.Errorf("Cursor rejected the request as invalid_argument")

func isInvalidArgument(err error) bool {
	return err != nil && (errors.Is(err, ErrInvalidArgument) || strings.Contains(strings.ToLower(err.Error()), "invalid_argument"))
}
