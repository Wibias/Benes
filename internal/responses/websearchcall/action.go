package websearchcall

import (
	"errors"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const ItemType = "web_search_call"

var ErrMalformedAction = errors.New("web_search_call action is malformed")

func Action(queries []string) map[string]any {
	query := ""
	if len(queries) > 0 {
		query = queries[0]
	}
	list := []string{query}
	if len(queries) > 1 {
		list = append([]string(nil), queries...)
	}
	return map[string]any{"type": "search", "query": query, "queries": list}
}

func Heal(action map[string]any) (string, []string, error) {
	if action == nil {
		return "", nil, ErrMalformedAction
	}
	if rawType, exists := action["type"]; exists {
		typeName, ok := rawType.(string)
		if !ok || (typeName != "" && typeName != "search") {
			return "", nil, ErrMalformedAction
		}
	}

	_, queryPresent := action["query"]
	query, hasQuery, err := optionalQuery(action["query"], queryPresent)
	if err != nil {
		return "", nil, err
	}
	queries, hasQueries, err := optionalQueries(action)
	if err != nil {
		return "", nil, err
	}

	switch {
	case hasQueries && len(queries) == 0:
		if !hasQuery {
			return "", nil, ErrMalformedAction
		}
		return query, []string{query}, nil
	case hasQueries && hasQuery:
		if query != queries[0] {
			return "", nil, ErrMalformedAction
		}
		return query, queries, nil
	case hasQueries:
		return queries[0], queries, nil
	case hasQuery:
		return query, []string{query}, nil
	default:
		return "", nil, ErrMalformedAction
	}
}

func IsHosted(part protocol.ContentPart) bool {
	return part.Type == protocol.ContentToolCall && part.CustomWireName == ItemType
}

func QueriesFromArguments(args map[string]any) []string {
	if args == nil {
		return nil
	}
	if raw, ok := args["queries"]; ok {
		if list, ok := stringList(raw); ok && len(list) > 0 {
			return list
		}
	}
	if query, ok := args["query"].(string); ok {
		return []string{query}
	}
	return nil
}

func HostedPart(id, query string, queries []string) protocol.ContentPart {
	id = strings.TrimSpace(id)
	if len(queries) == 0 {
		queries = []string{query}
	}
	if query == "" && len(queries) > 0 {
		query = queries[0]
	}
	return protocol.ContentPart{
		Type:           protocol.ContentToolCall,
		ToolCallID:     id,
		ToolName:       "web_search",
		CustomWireName: ItemType,
		Arguments:      map[string]any{"query": query, "queries": append([]string(nil), queries...)},
	}
}

func optionalQuery(raw any, present bool) (string, bool, error) {
	if !present {
		return "", false, nil
	}
	query, ok := raw.(string)
	if !ok {
		return "", false, ErrMalformedAction
	}
	return query, true, nil
}

func optionalQueries(action map[string]any) ([]string, bool, error) {
	raw, exists := action["queries"]
	if !exists || raw == nil {
		return nil, false, nil
	}
	list, ok := stringList(raw)
	if !ok {
		return nil, true, ErrMalformedAction
	}
	return list, true, nil
}

func stringList(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}
