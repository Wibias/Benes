package cursor

import (
	"encoding/base64"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestEncodeRunPayloadIsAgentClientMessage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nxxxx")
	req := RunRequest{
		Model:  "gpt-5.4",
		Turns:  []RunTurn{{Role: "user", Text: "hello cursor"}},
		Digest: "d1",
		Images: []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(png)},
	}
	raw := EncodeRunPayload(req)
	root, err := decodeProtoFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	run := fieldBytes(root, 1)
	if len(run) == 0 {
		t.Fatalf("missing run_request: %x", raw)
	}
	runFields, err := decodeProtoFields(run)
	if err != nil {
		t.Fatal(err)
	}
	details, err := decodeProtoFields(fieldBytes(runFields, 3))
	if err != nil || string(fieldBytes(details, 1)) != "gpt-5.4" {
		t.Fatalf("model_details=%x err=%v", fieldBytes(runFields, 3), err)
	}
	action, err := decodeProtoFields(fieldBytes(runFields, 2))
	if err != nil {
		t.Fatal(err)
	}
	userAction, err := decodeProtoFields(fieldBytes(action, 1))
	if err != nil {
		t.Fatal(err)
	}
	user, err := decodeProtoFields(fieldBytes(userAction, 1))
	if err != nil {
		t.Fatal(err)
	}
	if string(fieldBytes(user, 1)) != "hello cursor" {
		t.Fatalf("user text=%q", fieldBytes(user, 1))
	}
	ctx, err := decodeProtoFields(fieldBytes(user, 3))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeProtoFields(fieldBytes(ctx, 1))
	if err != nil || string(fieldBytes(img, 8)) != string(png) {
		t.Fatalf("selected image=%x err=%v", fieldBytes(ctx, 1), err)
	}
}

func TestEncodeSelectedImageUsesRawBytesField(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nxxxx")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	raw, err := EncodeSelectedImage(dataURL)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := decodeProtoFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(fieldBytes(fields, 7)) != "image/png" {
		t.Fatalf("mime=%q", fieldBytes(fields, 7))
	}
	if string(fieldBytes(fields, 8)) != string(png) {
		t.Fatalf("data=%x", fieldBytes(fields, 8))
	}
}

func TestDecodeServerFrameReadsTextAndUsage(t *testing.T) {
	text := EncodeProtoMessage(1, EncodeProtoMessage(1, EncodeProtoString(1, "delta")))
	ev, ok := DecodeServerFrame(text)
	if !ok || ev.Type != protocol.EventTextDelta || ev.Text != "delta" {
		t.Fatalf("text=%#v ok=%v", ev, ok)
	}
	tokens := EncodeProtoMessage(1, EncodeProtoMessage(8, EncodeProtoVarint(1, 4)))
	done, ok := DecodeServerFrame(tokens)
	if !ok || done.Type != protocol.EventDone || done.Usage == nil || done.Usage.OutputTokens != 4 || done.Usage.TotalTokens != 4 {
		t.Fatalf("usage=%#v ok=%v", done, ok)
	}
}
