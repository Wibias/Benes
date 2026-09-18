package config

import (
	"bytes"
	"encoding/json"
)

func scrubPersistedSecrets(root map[string]json.RawMessage) error {
	if root == nil {
		return nil
	}
	raw, ok := root["providers"]
	if !ok {
		return nil
	}
	var providers map[string]json.RawMessage
	if json.Unmarshal(raw, &providers) != nil || providers == nil {
		return nil
	}
	changed := false
	for id, provider := range providers {
		scrubbed, did, err := scrubProviderJSON(provider)
		if err != nil {
			return err
		}
		if did {
			providers[id] = scrubbed
			changed = true
		}
	}
	if !changed {
		return nil
	}
	encoded, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	root["providers"] = encoded
	return nil
}

func scrubProviderJSON(raw json.RawMessage) (json.RawMessage, bool, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw, false, nil
	}
	if _, hasRef := obj["credentialRef"]; !hasRef {
		return raw, false, nil
	}
	changed := false
	if _, ok := obj["apiKey"]; ok {
		delete(obj, "apiKey")
		changed = true
	}
	if pool, ok := obj["apiKeyPool"]; ok {
		scrubbed, did := scrubAPIKeyPoolSecrets(pool, true)
		if did {
			obj["apiKeyPool"] = scrubbed
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false, err
	}
	return out, true, nil
}

func scrubAPIKeyPoolSecrets(raw json.RawMessage, stripKeys bool) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, false
	}
	var entries []map[string]json.RawMessage
	if json.Unmarshal(trimmed, &entries) != nil {
		return raw, false
	}
	changed := false
	for i, entry := range entries {
		if entry == nil {
			continue
		}
		_, hasRef := entry["ref"]
		if !stripKeys && !hasRef {
			continue
		}
		if _, hasKey := entry["key"]; hasKey {
			delete(entry, "key")
			changed = true
			entries[i] = entry
		}
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return raw, false
	}
	return out, true
}
