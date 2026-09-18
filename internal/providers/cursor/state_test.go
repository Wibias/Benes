package cursor

import (
	"strings"
	"testing"
)

func TestConversationStateUsesSHA256BlobIDsAndOmitsActiveUser(t *testing.T) {
	req := RunRequest{
		System: []string{"sys"},
		Turns: []RunTurn{
			{Role: "user", Text: "first"},
			{Role: "assistant", Text: "ok"},
			{Role: "user", Text: "second"},
		},
	}
	blobs := RootPromptBlobs(req)
	if len(blobs) != 3 {
		t.Fatalf("blobs=%d", len(blobs))
	}
	state := encodeConversationState(req)
	fields, err := decodeProtoFields(state)
	if err != nil {
		t.Fatal(err)
	}
	var ids [][]byte
	for _, field := range fields {
		if field.num == 1 {
			ids = append(ids, field.bytes)
		}
	}
	if len(ids) != 3 || len(ids[0]) != 32 {
		t.Fatalf("ids=%d first=%d", len(ids), len(ids[0]))
	}
	if string(ids[0]) != string(BlobID(blobs[0])) {
		t.Fatal("system blob id mismatch")
	}
	for _, blob := range blobs {
		if strings.Contains(string(blob), "second") {
			t.Fatalf("active user leaked into roots: %s", blob)
		}
	}
}
