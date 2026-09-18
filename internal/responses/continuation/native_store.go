package continuation

import "strings"

const nativeCodexDestination = "https://chatgpt.com/backend-api/codex"

// ApplyNativeStoreDefault preserves an explicit client value. When store is
// omitted, only the canonical native Codex Responses forward destination gets
// the compatibility default store:false. Custom/key-auth gateways keep omission.
func ApplyNativeStoreDefault(explicit *bool, destination, adapter, authClass string) *bool {
	if explicit != nil {
		value := *explicit
		return &value
	}
	if adapter != "openai-responses" || authClass != "forward" {
		return nil
	}
	got, err := nativeStoreDestination(destination)
	if err != nil {
		return nil
	}
	want, err := nativeStoreDestination(nativeCodexDestination)
	if err != nil || got != want {
		return nil
	}
	value := false
	return &value
}

func nativeStoreDestination(raw string) (string, error) {
	canonical, err := canonicalDestination(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(canonical, "/responses"), nil
}
