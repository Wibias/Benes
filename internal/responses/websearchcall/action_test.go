package websearchcall

import (
	"errors"
	"reflect"
	"testing"
)

func TestActionAlwaysEmitsQueryAndQueries(t *testing.T) {
	tests := []struct {
		name    string
		queries []string
		query   string
		want    []string
	}{
		{name: "empty", query: "", want: []string{""}},
		{name: "single", queries: []string{"benes network"}, query: "benes network", want: []string{"benes network"}},
		{name: "multi", queries: []string{"one", "two"}, query: "one", want: []string{"one", "two"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Action(tc.queries)
			if got["type"] != "search" {
				t.Fatalf("type=%#v", got["type"])
			}
			if got["query"] != tc.query {
				t.Fatalf("query=%#v want %q", got["query"], tc.query)
			}
			if !reflect.DeepEqual(got["queries"], tc.want) {
				t.Fatalf("queries=%#v want %#v", got["queries"], tc.want)
			}
		})
	}
}

func TestHealCanonicalizesValidQueryShapes(t *testing.T) {
	tests := []struct {
		name    string
		action  map[string]any
		query   string
		queries []string
	}{
		{name: "query only", action: map[string]any{"query": "alpha"}, query: "alpha", queries: []string{"alpha"}},
		{name: "queries only", action: map[string]any{"queries": []any{"one", "two"}}, query: "one", queries: []string{"one", "two"}},
		{name: "both", action: map[string]any{"query": "one", "queries": []any{"one", "two"}}, query: "one", queries: []string{"one", "two"}},
		{name: "empty queries keeps query", action: map[string]any{"query": "legacy", "queries": []any{}}, query: "legacy", queries: []string{"legacy"}},
		{name: "empty generated shape", action: map[string]any{"query": "", "queries": []any{""}}, query: "", queries: []string{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, queries, err := Heal(tc.action)
			if err != nil {
				t.Fatal(err)
			}
			if query != tc.query || !reflect.DeepEqual(queries, tc.queries) {
				t.Fatalf("query=%q queries=%#v", query, queries)
			}
		})
	}
}

func TestHealFailsClosedOnMalformedOrContradictoryActions(t *testing.T) {
	tests := []struct {
		name   string
		action map[string]any
	}{
		{name: "nil action"},
		{name: "missing fields", action: map[string]any{"type": "search"}},
		{name: "empty queries", action: map[string]any{"queries": []any{}}},
		{name: "non-string query", action: map[string]any{"query": 42}},
		{name: "queries not array", action: map[string]any{"queries": "alpha"}},
		{name: "non-string queries", action: map[string]any{"queries": []any{42}}},
		{name: "mixed queries", action: map[string]any{"queries": []any{"a", 42}}},
		{name: "object queries", action: map[string]any{"queries": []any{map[string]any{"q": "x"}}}},
		{name: "contradiction", action: map[string]any{"query": "a", "queries": []any{"b", "c"}}},
		{name: "unknown type", action: map[string]any{"type": "open_page", "query": "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Heal(tc.action)
			if !errors.Is(err, ErrMalformedAction) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
