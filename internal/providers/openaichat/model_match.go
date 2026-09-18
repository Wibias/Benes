package openaichat

import "strings"

func modelMatchesConfiguredFamily(models []string, modelID string) bool {
	for _, configured := range models {
		if configured == modelID {
			return true
		}
	}
	colon := strings.IndexByte(modelID, ':')
	if colon <= 0 {
		return false
	}
	family := modelID[:colon]
	for _, configured := range models {
		if configured == family {
			return true
		}
	}
	return false
}
