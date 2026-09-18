package claude

import (
	"encoding/json"
	"strings"
)

func ContextWindowsFromModelsJSON(payload []byte) map[string]int {
	var envelope struct {
		Data []struct {
			ID            string `json:"id"`
			ContextWindow int    `json:"context_window"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return nil
	}
	out := map[string]int{}
	for _, row := range envelope.Data {
		id := strings.TrimSpace(row.ID)
		if id == "" || row.ContextWindow <= 0 {
			continue
		}
		out[id] = row.ContextWindow
		if slash := strings.LastIndex(id, "/"); slash >= 0 && slash < len(id)-1 {
			out[id[slash+1:]] = row.ContextWindow
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
