package openairesponses

import (
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

var openCodeGoMuseStrictWebSearchModels = map[string]struct{}{
	"muse-spark-1.3-contributor": {},
	"muse-spark-1.2-contributor": {},
}

func SanitizeOpenCodeGoMuseWebSearch(request protocol.ParsedRequest, destination string) protocol.ParsedRequest {
	if !canonicalOpenCodeGoResponsesDestination(destination) {
		return request
	}
	model := strings.TrimSpace(request.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(request.ModelID)
	}
	if _, ok := openCodeGoMuseStrictWebSearchModels[model]; !ok || len(request.HostedWebSearchTools) == 0 {
		return request
	}

	cloned := append([]protocol.HostedWebSearchTool(nil), request.HostedWebSearchTools...)
	changed := false
	for index := range cloned {
		if cloned[index].Type != protocol.HostedWebSearchWebSearch {
			continue
		}
		if cloned[index].SearchContentTypes == nil && cloned[index].IndexedWebAccess == nil {
			continue
		}
		cloned[index].SearchContentTypes = nil
		cloned[index].IndexedWebAccess = nil
		changed = true
	}
	if changed {
		request.HostedWebSearchTools = cloned
	}
	return request
}

func canonicalOpenCodeGoResponsesDestination(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return false
	}
	return strings.TrimRight(parsed.EscapedPath(), "/") == "/zen/go/v1/responses"
}
