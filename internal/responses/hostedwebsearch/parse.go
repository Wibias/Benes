package hostedwebsearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

var hostedFields = map[string]struct{}{
	"type":                 {},
	"search_context_size":  {},
	"search_content_types": {},
	"indexed_web_access":   {},
	"external_web_access":  {},
	"user_location":        {},
	"filters":              {},
}

var locationFields = map[string]struct{}{
	"type": {}, "country": {}, "city": {}, "region": {}, "timezone": {},
}

var filterFields = map[string]struct{}{
	"allowed_domains": {},
}

func Parse(raw json.RawMessage) (protocol.HostedWebSearchTool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return protocol.HostedWebSearchTool{}, fmt.Errorf("hosted web search tool must be an object")
	}
	for key := range fields {
		if _, ok := hostedFields[key]; !ok {
			return protocol.HostedWebSearchTool{}, fmt.Errorf("unsupported hosted web search field %q", key)
		}
	}

	typeName, err := requiredString(fields, "type")
	if err != nil {
		return protocol.HostedWebSearchTool{}, err
	}
	out := protocol.HostedWebSearchTool{Type: protocol.HostedWebSearchKind(typeName)}
	switch out.Type {
	case protocol.HostedWebSearchWebSearch, protocol.HostedWebSearchPreview:
	default:
		return protocol.HostedWebSearchTool{}, fmt.Errorf("unsupported hosted web search type %q", typeName)
	}

	if rawValue, ok := fields["search_context_size"]; ok {
		value, err := stringValue(rawValue, "search_context_size")
		if err != nil {
			return protocol.HostedWebSearchTool{}, err
		}
		switch value {
		case "low", "medium", "high":
			out.SearchContextSize = value
		default:
			return protocol.HostedWebSearchTool{}, fmt.Errorf("invalid search_context_size %q", value)
		}
	}
	if rawValue, ok := fields["search_content_types"]; ok {
		values, err := stringList(rawValue, "search_content_types", 2)
		if err != nil {
			return protocol.HostedWebSearchTool{}, err
		}
		for _, value := range values {
			switch value {
			case "text", "image":
			default:
				return protocol.HostedWebSearchTool{}, fmt.Errorf("unsupported search_content_types value %q", value)
			}
		}
		out.SearchContentTypes = values
	}
	if rawValue, ok := fields["indexed_web_access"]; ok {
		if out.Type == protocol.HostedWebSearchPreview {
			return protocol.HostedWebSearchTool{}, fmt.Errorf("indexed_web_access is not supported by web_search_preview")
		}
		value, err := boolValue(rawValue, "indexed_web_access")
		if err != nil {
			return protocol.HostedWebSearchTool{}, err
		}
		out.IndexedWebAccess = &value
	}
	if rawValue, ok := fields["external_web_access"]; ok {
		if out.Type == protocol.HostedWebSearchPreview {
			return protocol.HostedWebSearchTool{}, fmt.Errorf("external_web_access is not supported by web_search_preview")
		}
		value, err := boolValue(rawValue, "external_web_access")
		if err != nil {
			return protocol.HostedWebSearchTool{}, err
		}
		out.ExternalWebAccess = &value
	}
	if rawValue, ok := fields["user_location"]; ok {
		location, err := parseLocation(rawValue)
		if err != nil {
			return protocol.HostedWebSearchTool{}, err
		}
		out.UserLocation = location
	}
	if rawValue, ok := fields["filters"]; ok {
		if out.Type == protocol.HostedWebSearchPreview {
			return protocol.HostedWebSearchTool{}, fmt.Errorf("filters are not supported by web_search_preview")
		}
		filters, err := parseFilters(rawValue)
		if err != nil {
			return protocol.HostedWebSearchTool{}, err
		}
		out.Filters = filters
	}
	return out, nil
}

func ParseDeclared(tools []json.RawMessage) ([]protocol.HostedWebSearchTool, error) {
	var out []protocol.HostedWebSearchTool
	for index, raw := range tools {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("tools[%d] must be an object", index)
		}
		typeName, err := requiredString(fields, "type")
		if err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", index, err)
		}
		switch typeName {
		case string(protocol.HostedWebSearchWebSearch), string(protocol.HostedWebSearchPreview):
			tool, err := Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("tools[%d]: %w", index, err)
			}
			out = append(out, tool)
		default:
			if strings.HasPrefix(typeName, "web_search") {
				return nil, fmt.Errorf("tools[%d]: unsupported hosted web search type %q", index, typeName)
			}
		}
	}
	return out, nil
}

func parseLocation(raw json.RawMessage) (*protocol.HostedWebSearchLocation, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("user_location must be an object")
	}
	for key := range fields {
		if _, ok := locationFields[key]; !ok {
			return nil, fmt.Errorf("unsupported user_location field %q", key)
		}
	}
	typeName, err := requiredString(fields, "type")
	if err != nil || typeName != "approximate" {
		return nil, fmt.Errorf("user_location.type must be \"approximate\"")
	}
	out := &protocol.HostedWebSearchLocation{Type: typeName}
	for key, target := range map[string]*string{
		"country": &out.Country, "city": &out.City, "region": &out.Region, "timezone": &out.Timezone,
	} {
		if rawValue, ok := fields[key]; ok {
			value, err := stringValue(rawValue, "user_location."+key)
			if err != nil {
				return nil, err
			}
			*target = value
		}
	}
	return out, nil
}

func parseFilters(raw json.RawMessage) (*protocol.HostedWebSearchFilters, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("filters must be an object")
	}
	for key := range fields {
		if _, ok := filterFields[key]; !ok {
			return nil, fmt.Errorf("unsupported filters field %q", key)
		}
	}
	out := &protocol.HostedWebSearchFilters{}
	var err error
	if rawValue, ok := fields["allowed_domains"]; ok {
		out.AllowedDomains, err = stringList(rawValue, "filters.allowed_domains", 100)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("requires %s", key)
	}
	return stringValue(raw, key)
}

func stringValue(raw json.RawMessage, name string) (string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("%s must be a string", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value, nil
}

func boolValue(raw json.RawMessage, name string) (bool, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func stringList(raw json.RawMessage, name string, max int) ([]string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%s must be a string array", name)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be a string array", name)
	}
	if max > 0 && len(values) > max {
		return nil, fmt.Errorf("%s exceeds %d entries", name, max)
	}
	return values, nil
}
