package cursor

import (
	"errors"
	"strings"
	"testing"
)

func TestBlobStoreServesGetBlobBySHA256(t *testing.T) {
	store := NewBlobStore()
	req := RunRequest{System: []string{"sys"}, Turns: []RunTurn{{Role: "user", Text: "hi"}}}
	if err := store.PutRoots(req); err != nil {
		t.Fatal(err)
	}
	blobs := RootPromptBlobs(req)
	got, ok := store.Get(BlobID(blobs[0]))
	if !ok || string(got) != string(blobs[0]) {
		t.Fatalf("get=%q ok=%v", got, ok)
	}
	args := EncodeProtoMessage(4, append(EncodeProtoVarint(1, 9), EncodeProtoMessage(2, EncodeProtoBytes(1, BlobID(blobs[0])))...))
	id, blobID, ok := ParseGetBlobArgs(args)
	if !ok || id != 9 || string(blobID) != string(BlobID(blobs[0])) {
		t.Fatalf("parse id=%d ok=%v", id, ok)
	}
	reply := EncodeGetBlobResult(id, got)
	root, err := decodeProtoFields(reply)
	if err != nil {
		t.Fatal(err)
	}
	kv, err := decodeProtoFields(fieldBytes(root, 3))
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeProtoFields(fieldBytes(kv, 2))
	if err != nil || string(fieldBytes(result, 1)) != string(blobs[0]) {
		t.Fatalf("reply=%x err=%v", reply, err)
	}
}

func TestPutConversationTurnFailsClosedWithoutTurnOnOverCountAndOverBytes(t *testing.T) {
	store := NewBlobStore()
	tooMany := make([]string, maxReplayRoots+1)
	for i := range tooMany {
		tooMany[i] = "s"
	}
	err := store.PutConversationTurn(RunRequest{System: tooMany, Turns: []RunTurn{{Role: "user", Text: "hi"}}}, nil)
	if !errors.Is(err, ErrReplayRootLimit) {
		t.Fatalf("over-count err=%v", err)
	}
	if len(store.IDs()) != 0 {
		t.Fatalf("stored roots after over-count: %d", len(store.IDs()))
	}

	err = store.PutConversationTurn(RunRequest{
		System: []string{strings.Repeat("x", maxReplayBytes+1)},
		Turns:  []RunTurn{{Role: "user", Text: "hi"}},
	}, nil)
	if !errors.Is(err, ErrReplayByteLimit) {
		t.Fatalf("over-bytes err=%v", err)
	}
	if len(store.IDs()) != 0 {
		t.Fatalf("stored roots after over-bytes: %d", len(store.IDs()))
	}
}
