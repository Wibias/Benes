package codexrouting

import "testing"

func TestEffectiveBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "none", in: "model_provider = \"openai\"\n", want: ""},
		{name: "root", in: "openai_base_url = \"http://127.0.0.1:23100/v1\"\n", want: "http://127.0.0.1:23100/v1"},
		{name: "benes table", in: "model_provider = \"benes\"\n[model_providers.benes]\nbase_url = \"http://127.0.0.1:23100/v1\"\n", want: "http://127.0.0.1:23100/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveBaseURL(tc.in); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestPublicRoute(t *testing.T) {
	if PublicRoute(KindBenesLocal) != "benes" || PublicRoute(KindNative) != "native" {
		t.Fatal("benes/native")
	}
	if PublicRoute(KindCustomLocal) != "other" || PublicRoute(KindCustomRemote) != "other" {
		t.Fatal("other")
	}
	if PublicRoute(KindUnknown) != "unknown" {
		t.Fatal("unknown")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{name: "empty", in: "", want: KindNative},
		{name: "native openai", in: "model_provider = \"openai\"\n", want: KindNative},
		{name: "injected loopback", in: Marker + "\nopenai_base_url = \"http://127.0.0.1:23100/v1\"\n", want: KindBenesLocal},
		{name: "custom local", in: "openai_base_url = \"http://127.0.0.1:8080/v1\"\n", want: KindCustomLocal},
		{name: "custom remote", in: "openai_base_url = \"https://api.example.com/v1\"\n", want: KindCustomRemote},
		{name: "benes table", in: "model_provider = \"benes\"\n[model_providers.benes]\nbase_url = \"http://127.0.0.1:23100/v1\"\n", want: KindBenesLocal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.in); got != tc.want {
				t.Fatalf("got=%s want=%s", got, tc.want)
			}
		})
	}
}
