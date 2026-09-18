package parsed

import (
	"bytes"
	"testing"

	"github.com/Wibias/Benes/internal/responses/request"
)

func TestBuildPreservesExplicitStorePresenceAndOmission(t *testing.T) {
	decoded, err := request.Decode(bytes.NewBufferString(`{"model":"openai/gpt-test","input":"hi","store":false}`), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Build(decoded, 1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Options.Store == nil || *parsed.Options.Store {
		t.Fatalf("explicit store:false lost: %#v", parsed.Options.Store)
	}

	decoded, err = request.Decode(bytes.NewBufferString(`{"model":"openai/gpt-test","input":"hi","store":true}`), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = Build(decoded, 1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Options.Store == nil || !*parsed.Options.Store {
		t.Fatalf("explicit store:true lost: %#v", parsed.Options.Store)
	}

	decoded, err = request.Decode(bytes.NewBufferString(`{"model":"openai/gpt-test","input":"hi"}`), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = Build(decoded, 1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Options.Store != nil {
		t.Fatalf("omitted store became explicit: %#v", parsed.Options.Store)
	}
}
