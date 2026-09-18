package kiro

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestExtractImagesAcceptsDataOnlyAndCapsCount(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nxxxx"))
	got, err := ExtractImages([]protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/png;base64," + png}})
	if err != nil || len(got) != 1 || got[0].Format != "png" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if _, err := ExtractImages([]protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "https://example/x.png"}}); err == nil {
		t.Fatal("remote")
	}
	parts := make([]protocol.ContentPart, 21)
	for i := range parts {
		parts[i] = protocol.ContentPart{Type: protocol.ContentImage, ImageURL: "data:image/png;base64," + png}
	}
	if _, err := ExtractImages(parts); err == nil || !strings.Contains(err.Error(), "count") {
		t.Fatalf("count=%v", err)
	}
}
