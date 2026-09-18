package parsed

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
)

func TestBuildMarksResponsesSourceAndCanonicalizesUserMetadata(t *testing.T) {
	got, err := Build(decode(t, `{"model":"p/m","user":"user-1","metadata":{"trace":"abc"},"input":"hi"}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != protocol.RequestSourceResponses {
		t.Fatalf("source=%q", got.Source)
	}
	if got.Options.User == nil || *got.Options.User != "user-1" || got.Options.Metadata["trace"] != "abc" {
		t.Fatalf("options=%#v", got.Options)
	}
}

func TestBuildRejectsNonObjectMetadata(t *testing.T) {
	req, err := requestDecode(strings.NewReader(`{"model":"p/m","metadata":["not","object"],"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(req, 1); err == nil {
		t.Fatal("non-object metadata accepted")
	}
}

func requestDecode(reader *strings.Reader) (*requestwire.Request, error) {
	return requestwire.Decode(reader, 1<<20)
}
