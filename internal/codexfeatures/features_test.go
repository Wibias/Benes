package codexfeatures

import "testing"

func TestEnabledFromText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "empty", in: "", want: false},
		{name: "bool true", in: "[features]\nmulti_agent_v2 = true\n", want: true},
		{name: "bool false", in: "[features]\nmulti_agent_v2 = false\n", want: false},
		{name: "table true", in: "[features.multi_agent_v2]\nenabled = true\n", want: true},
		{name: "table false", in: "[features.multi_agent_v2]\nenabled = false\n", want: false},
		{name: "inline true", in: "[features]\nmulti_agent_v2 = { enabled = true }\n", want: true},
		{name: "other table", in: "[other]\nenabled = true\n", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EnabledFromText(tc.in); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}
