package config

import "testing"

func TestOpenAIDefaultConnectionIsAuthoritative(t *testing.T) {
	tests := []struct {
		name          string
		defaultAccess string
		want          string
	}{
		{name: "oauth", defaultAccess: DefaultAccessOAuth, want: LogicalOpenAIID},
		{name: "api", defaultAccess: DefaultAccessAPI, want: OpenAIAPIConnection},
		{name: "empty defaults to oauth", defaultAccess: "", want: LogicalOpenAIID},
		{name: "unknown defaults to oauth", defaultAccess: "unexpected", want: LogicalOpenAIID},
		{name: "whitespace api", defaultAccess: "  api  ", want: OpenAIAPIConnection},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OpenAIDefaultConnection(tt.defaultAccess); got != tt.want {
				t.Fatalf("OpenAIDefaultConnection(%q)=%q want=%q", tt.defaultAccess, got, tt.want)
			}
		})
	}
}
