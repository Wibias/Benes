package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func validateNativeTopLevelTool(raw json.RawMessage, path string) error {
	return validateNativeToolSpec(raw, path, false)
}

func validateNativeNamespaceTool(raw json.RawMessage, path string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedRequestShape, path)
	}
	if rawString(fields["type"]) != "namespace" || strings.TrimSpace(rawString(fields["name"])) == "" {
		return fmt.Errorf("%w: %s", ErrUnsupportedRequestShape, path)
	}
	childrenRaw := bytes.TrimSpace(fields["tools"])
	if len(childrenRaw) == 0 || bytes.Equal(childrenRaw, []byte("null")) {
		return fmt.Errorf("%w: %s.tools", ErrUnsupportedRequestShape, path)
	}
	var children []json.RawMessage
	if err := json.Unmarshal(childrenRaw, &children); err != nil {
		return fmt.Errorf("%w: %s.tools", ErrUnsupportedRequestShape, path)
	}
	for index, child := range children {
		if err := validateNativeToolSpec(child, fmt.Sprintf("%s.tools[%d]", path, index), true); err != nil {
			return err
		}
	}
	return nil
}

func validateNativeToolSpec(raw json.RawMessage, path string, nested bool) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedRequestShape, path)
	}
	switch rawString(fields["type"]) {
	case "function":
		return nil
	case "custom":
		if nested {
			return nil
		}
	case "namespace":
		if !nested {
			return validateNativeNamespaceTool(raw, path)
		}
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedRequestShape, path)
}
