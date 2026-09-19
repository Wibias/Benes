package continuation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestAuthorityBindsPreviousResponseAfterFinalOwnerAndDedupsCarriedPrefix(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()
	owner, durable, err := testIdentity().Resolve(authority.salt, authority.processSalt)
	if err != nil || !durable {
		t.Fatalf("owner=%v durable=%v err=%v", owner, durable, err)
	}

	first := userRequest("ask")
	if err := authority.Remember(owner, true, first, completedResponse("resp_1", "msg-1", "answer")); err != nil {
		t.Fatal(err)
	}

	carried := protocol.ParsedRequest{
		PreviousResponseID: "resp_1",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ask"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "answer", ItemID: "msg-1"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	}
	bound, err := authority.Bind(BindRequest{Identity: testIdentity(), Turn: turn, Request: carried})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Release()
	if !bound.Hit || bound.Expanded || bound.Request.PreviousResponseID != "resp_1" {
		t.Fatalf("dedup bind=%#v", bound)
	}
	if bound.ReplayPrefixLength == 0 || bound.Request.ReplayPrefixLength != bound.ReplayPrefixLength {
		t.Fatal("replay prefix provenance was not recorded")
	}
	if got := len(bound.Request.Context.Messages); got != 3 {
		t.Fatalf("carried history was compounded: %d", got)
	}
}

func TestAuthorityExpandsMissingPrefixAndFailsClosedOnForeignOwner(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()
	owner, _, err := testIdentity().Resolve(authority.salt, authority.processSalt)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Remember(owner, true, userRequest("ask"), completedResponse("resp_1", "msg-1", "answer")); err != nil {
		t.Fatal(err)
	}

	delta := userRequest("next")
	delta.PreviousResponseID = "resp_1"
	bound, err := authority.Bind(BindRequest{Identity: testIdentity(), Turn: turn, Request: delta})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Release()
	if !bound.Hit || !bound.Expanded || len(bound.Request.Context.Messages) < 2 {
		t.Fatalf("missing prefix was not expanded: %#v messages=%d", bound, len(bound.Request.Context.Messages))
	}

	foreign := testIdentity()
	foreign.Secret = []byte("sk-other")
	miss, err := authority.Bind(BindRequest{Identity: foreign, Turn: turn, Request: delta})
	if err != nil {
		t.Fatal(err)
	}
	defer miss.Release()
	if miss.Hit {
		t.Fatal("foreign credential reused continuation state")
	}
}

func TestAuthorityStripsNativePreviousResponseOnMissAndKeepsOnHit(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()
	owner, _, _ := testIdentity().Resolve(authority.salt, authority.processSalt)
	_ = authority.Remember(owner, true, userRequest("ask"), completedResponse("resp_1", "msg-1", "answer"))

	miss, err := authority.Bind(BindRequest{
		Identity:            testIdentity(),
		Turn:                turn,
		Request:             protocol.ParsedRequest{PreviousResponseID: "resp_missing", Context: userRequest("next").Context},
		StripPreviousOnMiss: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer miss.Release()
	if miss.Request.PreviousResponseID != "" {
		t.Fatal("native miss kept previous_response_id")
	}

	hit, err := authority.Bind(BindRequest{
		Identity:            testIdentity(),
		Turn:                turn,
		Request:             protocol.ParsedRequest{PreviousResponseID: "resp_1", Context: userRequest("next").Context},
		StripPreviousOnMiss: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Release()
	if !hit.Hit || hit.Request.PreviousResponseID != "resp_1" {
		t.Fatalf("native hit lost previous_response_id: %#v", hit)
	}
}

func TestAuthorityThoughtSignaturesAreOwnerFencedAndConflictClosed(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()
	owner, durable, err := testIdentity().Resolve(authority.salt, authority.processSalt)
	if err != nil || !durable {
		t.Fatal(err)
	}
	if err := authority.RememberSignature(owner, true, "thread-1", "call-1", "sig-a"); err != nil {
		t.Fatal(err)
	}
	if err := authority.RememberSignature(owner, true, "thread-1", "call-1", "sig-b"); err != nil {
		t.Fatal(err)
	}
	got, ok := authority.LookupSignature(owner, "thread-1", "call-1", turn)
	if !ok || got != "sig-a" {
		t.Fatalf("conflict did not keep first signature: %q %v", got, ok)
	}

	foreign, _, err := PhysicalIdentity{
		Provider: "openai", Destination: "https://api.openai.com/v1/responses",
		Adapter: "openai-responses", Model: "gpt", AuthClass: "api-key", Secret: []byte("sk-other"),
	}.Resolve(authority.salt, authority.processSalt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := authority.LookupSignature(foreign, "thread-1", "call-1", turn); ok {
		t.Fatal("foreign owner received thought signature")
	}

	request := protocol.ParsedRequest{
		PreviousResponseID: "thread-1",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleAssistant,
			Content: []protocol.ContentPart{{
				Type: protocol.ContentToolCall, ToolCallID: "call-1", ToolName: "lookup", Arguments: map[string]any{},
			}},
		}}},
	}
	bound, err := authority.Bind(BindRequest{Identity: testIdentity(), Thread: "thread-1", Turn: turn, Request: request})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Release()
	if thoughtSignatureFromPart(bound.Request.Context.Messages[0].Content[0]) != "sig-a" {
		t.Fatalf("signature was not attached after final owner bind: %#v", bound.Request.Context.Messages[0].Content[0])
	}

	foreignReq := protocol.ParsedRequest{
		PreviousResponseID: "thread-1",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleAssistant,
			Content: []protocol.ContentPart{{
				Type: protocol.ContentToolCall, ToolCallID: "call-1", ToolName: "lookup",
				ThoughtSignature: "sig-a",
			}},
		}}},
	}
	foreignID := testIdentity()
	foreignID.Secret = []byte("sk-other")
	stripped, err := authority.Bind(BindRequest{Identity: foreignID, Thread: "thread-1", Turn: turn, Request: foreignReq})
	if err != nil {
		t.Fatal(err)
	}
	defer stripped.Release()
	if thoughtSignatureFromPart(stripped.Request.Context.Messages[0].Content[0]) != "" {
		t.Fatal("unowned thought signature was replayed")
	}
}

func TestAuthorityDoesNotRestoreBeforeTurnLeaseAndReleasesBytes(t *testing.T) {
	authority, _ := testAuthority(t)
	owner, _, _ := testIdentity().Resolve(authority.salt, authority.processSalt)
	_ = authority.Remember(owner, true, userRequest("ask"), completedResponse("resp_1", "msg-1", "answer"))

	unleased, err := authority.Bind(BindRequest{
		Identity: testIdentity(),
		Request:  protocol.ParsedRequest{PreviousResponseID: "resp_1", Context: userRequest("next").Context},
	})
	if err != nil {
		t.Fatal(err)
	}
	if unleased.Hit {
		t.Fatal("continuation restored without a turn lease")
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 1, MaxProcessBytes: 1 << 20, MaxTurnBytes: 1 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1 << 20},
	})
	turn, err := budget.AcquireTurn(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := authority.Bind(BindRequest{Identity: testIdentity(), Turn: turn, Request: protocol.ParsedRequest{PreviousResponseID: "resp_1", Context: userRequest("next").Context}})
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Hit || budget.Metrics().Bytes[resourcebudget.ClassContinuation] == 0 {
		t.Fatal("active continuation did not lease turn bytes")
	}
	bound.Release()
	if budget.Metrics().Bytes[resourcebudget.ClassContinuation] != 0 {
		t.Fatal("continuation lease leaked")
	}
	turn.Close()
}

func TestRememberProviderStateIsOwnerScoped(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()
	owner, _, err := testIdentity().Resolve(authority.salt, authority.processSalt)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RememberProviderState(owner, false, "cursor-thread", json.RawMessage(`{"ckpt":1}`)); err != nil {
		t.Fatal(err)
	}
	bound, err := authority.Bind(BindRequest{
		Identity: testIdentity(),
		Turn:     turn,
		Request:  protocol.ParsedRequest{PreviousResponseID: "cursor-thread"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Release()
	if !bound.Hit || string(bound.ProviderPayload()) != `{"ckpt":1}` {
		t.Fatalf("hit=%v payload=%s", bound.Hit, bound.ProviderPayload())
	}
	other := testIdentity()
	other.Secret = []byte("other-key")
	miss, err := authority.Bind(BindRequest{
		Identity: other,
		Turn:     turn,
		Request:  protocol.ParsedRequest{PreviousResponseID: "cursor-thread"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer miss.Release()
	if miss.Hit {
		t.Fatal("foreign owner must not receive Cursor checkpoint state")
	}
}

func TestAuthorityEphemeralForwardIdentityIsProcessLocalAndNotDurable(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()
	identity := PhysicalIdentity{
		Provider: "openai", Destination: "https://chatgpt.com/backend-api/codex/responses",
		Adapter: "openai-responses", Model: "gpt-5", AuthClass: "forward", Secret: []byte("Bearer rotating"),
	}
	owner, durable, err := identity.Resolve(authority.salt, authority.processSalt)
	if err != nil || durable || !stringsHasPrefix(owner.Credential, "eph:") {
		t.Fatalf("ephemeral owner=%#v durable=%v err=%v", owner, durable, err)
	}
}

func TestAuthorityBoundedPersistVisibilityDoesNotBlockOnStall(t *testing.T) {
	store := NewStore(StoreLimits{}, time.Now)
	persister := newPersister(store, filepath.Join(t.TempDir(), "state.json"), func(string, []byte) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	authority, err := NewAuthority(store, persister, bytes32('s'))
	if err != nil {
		t.Fatal(err)
	}
	authority.persistWait = 20 * time.Millisecond
	owner, _, _ := testIdentity().Resolve(authority.salt, authority.processSalt)
	if err := authority.Remember(owner, true, userRequest("ask"), completedResponse("resp_1", "msg-1", "answer")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := authority.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Fatalf("settle blocked too long: %s", time.Since(start))
	}
}

func testAuthority(t *testing.T) (*Authority, *resourcebudget.Turn) {
	t.Helper()
	store := NewStore(StoreLimits{MaxEntryBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxEntries: 32, TTL: time.Hour}, time.Now)
	authority, err := NewAuthority(store, nil, bytes32('k'))
	if err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 4, MaxProcessBytes: 4 << 20, MaxTurnBytes: 4 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20},
	})
	turn, err := budget.AcquireTurn(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	return authority, turn
}

func testIdentity() PhysicalIdentity {
	return PhysicalIdentity{
		Provider:    "openai",
		Destination: "https://api.openai.com/v1/responses",
		Adapter:     "openai-responses",
		Model:       "gpt-5.6",
		AuthClass:   "api-key",
		Secret:      []byte("sk-test-secret"),
	}
}

func userRequest(text string) protocol.ParsedRequest {
	return protocol.ParsedRequest{
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: text}},
		}}},
	}
}

func completedResponse(id, messageID, text string) json.RawMessage {
	return json.RawMessage(`{"id":"` + id + `","status":"completed","output":[{"type":"message","id":"` + messageID + `","role":"assistant","content":[{"type":"output_text","text":"` + text + `"}]}]}`)
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func TestOAuthContinuationOwnerSurvivesTokenRotationButRejectsDifferentAccount(t *testing.T) {
	authority, turn := testAuthority(t)
	defer turn.Close()

	identityA := PhysicalIdentity{
		Provider: "provider", Destination: "https://provider.example/v1",
		Adapter: "responses", Model: "model", AuthClass: "oauth",
		AccountHandle: "account-a", Secret: []byte("token-old"),
	}
	ownerA, durable, err := identityA.Resolve(authority.salt, authority.processSalt)
	if err != nil || !durable {
		t.Fatalf("owner=%#v durable=%v err=%v", ownerA, durable, err)
	}
	if err := authority.RememberProviderState(ownerA, durable, "thread-1", json.RawMessage(`{"state":"owned"}`)); err != nil {
		t.Fatal(err)
	}

	rotatedA := identityA
	rotatedA.Secret = []byte("token-new")
	same, err := authority.Bind(BindRequest{
		Identity: rotatedA,
		Turn:     turn,
		Request:  protocol.ParsedRequest{PreviousResponseID: "thread-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer same.Release()
	if !same.Hit || string(same.ProviderPayload()) != `{"state":"owned"}` {
		t.Fatalf("same account token rotation lost state: hit=%v payload=%s owner=%#v", same.Hit, same.ProviderPayload(), same.Owner)
	}
	if !same.Owner.Matches(ownerA) {
		t.Fatalf("same account token rotation changed owner: %#v vs %#v", same.Owner, ownerA)
	}

	identityB := identityA
	identityB.AccountHandle = "account-b"
	identityB.Secret = []byte("token-b")
	foreign, err := authority.Bind(BindRequest{
		Identity: identityB,
		Turn:     turn,
		Request:  protocol.ParsedRequest{PreviousResponseID: "thread-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Release()
	if foreign.Hit || len(foreign.ProviderPayload()) != 0 {
		t.Fatalf("different OAuth account reused state: hit=%v payload=%s owner=%#v", foreign.Hit, foreign.ProviderPayload(), foreign.Owner)
	}
	if foreign.Owner.Matches(ownerA) {
		t.Fatalf("different OAuth account shared owner: %#v", foreign.Owner)
	}
}
