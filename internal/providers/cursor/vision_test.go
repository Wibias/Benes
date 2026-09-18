package cursor

import (
	"encoding/base64"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestPromoteActiveImagesAcceptsStrictDataOnly(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + "xxxx")
	ok := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	got, err := PromoteActiveImages(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: ok}},
		}}},
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("ok=%v err=%v", got, err)
	}
	if _, err := PromoteActiveImages(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "https://example/x.png"}},
		}}},
	}); err == nil {
		t.Fatal("remote URL must fail closed")
	}
	if imgs, err := PromoteActiveImages(protocol.ParsedRequest{UpstreamModelID: "mystery-router"}); err != nil || imgs != nil {
		t.Fatalf("unknown model=%v %v", imgs, err)
	}
}

func TestPromoteActiveImagesRejectsTruncatedBase64(t *testing.T) {
	_, err := PromoteActiveImages(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,abc"}},
		}}},
	})
	if err == nil {
		t.Fatal("truncated base64")
	}
}
