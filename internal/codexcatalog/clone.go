package codexcatalog

import "encoding/json"

func cloneEntry(entry map[string]any) map[string]any {
	if entry == nil {
		return nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil
	}
	return out
}
