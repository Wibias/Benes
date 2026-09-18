package continuation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

const (
	maxThoughtSignatureBytes = 64 * 1024
	defaultPersistWait       = 250 * time.Millisecond
)

type PhysicalIdentity struct {
	Provider      string
	Destination   string
	Adapter       string
	Model         string
	AuthClass     string
	Secret        []byte
	AccountHandle string
}

func (id PhysicalIdentity) Resolve(installationSalt, processSalt []byte) (Owner, bool, error) {
	var (
		credential string
		durable    bool
		err        error
	)
	handle := strings.TrimSpace(id.AccountHandle)
	switch {
	case handle != "":
		credential, err = DurableOAuthIdentity(handle)
		durable = true
	case strings.EqualFold(strings.TrimSpace(id.AuthClass), "api-key"):
		credential, err = DurableKeyIdentity(id.Secret, installationSalt)
		durable = true
	default:
		credential, err = EphemeralKeyIdentity(id.Secret, processSalt)
	}
	if err != nil {
		return Owner{}, false, err
	}
	owner, err := NewOwner(id.Provider, id.Destination, id.Adapter, id.Model, credential)
	if err != nil {
		return Owner{}, false, err
	}
	return owner, durable && DurableCredential(credential), nil
}

type Authority struct {
	store       *Store
	persister   *Persister
	salt        []byte
	processSalt []byte
	persistWait time.Duration
}

func NewAuthority(store *Store, persister *Persister, installationSalt []byte) (*Authority, error) {
	if store == nil {
		return nil, fmt.Errorf("continuation store is required")
	}
	if len(installationSalt) < 32 {
		return nil, ErrDurableIdentityUnavailable
	}
	processSalt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, processSalt); err != nil {
		return nil, fmt.Errorf("generate process continuation salt: %w", err)
	}
	return &Authority{
		store:       store,
		persister:   persister,
		salt:        append([]byte(nil), installationSalt...),
		processSalt: processSalt,
		persistWait: defaultPersistWait,
	}, nil
}

type BindRequest struct {
	Identity            PhysicalIdentity
	Thread              string
	Turn                *resourcebudget.Turn
	Request             protocol.ParsedRequest
	StripPreviousOnMiss bool
}

type Bound struct {
	Request            protocol.ParsedRequest
	Owner              Owner
	Durable            bool
	Hit                bool
	Expanded           bool
	ReplayPrefixLength int
	Entry              Entry

	authority *Authority
	lease     *Lease
}

func (b *Bound) Release() {
	if b == nil {
		return
	}
	if b.lease != nil {
		b.lease.Release()
		b.lease = nil
	}
}

func (b Bound) ProviderPayload() json.RawMessage {
	if len(b.Entry.Payload) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), b.Entry.Payload...)
}

func (a *Authority) InstallationSalt() []byte {
	if a == nil {
		return nil
	}
	return append([]byte(nil), a.salt...)
}

func (a *Authority) ProcessSalt() []byte {
	if a == nil {
		return nil
	}
	return append([]byte(nil), a.processSalt...)
}

func (a *Authority) OwnsPrevious(owner Owner, previousID string) bool {
	if a == nil || a.store == nil {
		return false
	}
	previousID = strings.TrimSpace(previousID)
	if previousID == "" || owner.Provider == "" {
		return false
	}
	_, ok := a.store.Peek(ReplayKey{Thread: previousID, CallID: PrefixCallID, Owner: owner}, owner)
	return ok
}

func (a *Authority) Bind(in BindRequest) (Bound, error) {
	if a == nil {
		return Bound{Request: in.Request}, nil
	}
	owner, durable, err := in.Identity.Resolve(a.salt, a.processSalt)
	if err != nil {
		request := in.Request
		if in.StripPreviousOnMiss {
			request.PreviousResponseID = ""
		}
		return Bound{Request: request, Durable: false}, nil
	}
	bound := Bound{
		Request:   in.Request,
		Owner:     owner,
		Durable:   durable,
		authority: a,
	}
	previousID := strings.TrimSpace(in.Request.PreviousResponseID)
	if previousID == "" {
		a.applySignatures(&bound, in.Thread, in.Turn)
		return bound, nil
	}
	if in.Turn == nil {
		if in.StripPreviousOnMiss {
			bound.Request.PreviousResponseID = ""
		}
		return bound, nil
	}
	key := ReplayKey{Thread: previousID, CallID: PrefixCallID, Owner: owner}
	lease, ok, err := a.store.Lease(key, owner, in.Turn)
	if err != nil {
		return Bound{}, err
	}
	if !ok {
		if in.StripPreviousOnMiss {
			bound.Request.PreviousResponseID = ""
		}
		a.applySignatures(&bound, firstNonEmpty(in.Thread, previousID), in.Turn)
		return bound, nil
	}
	entry := lease.Entry()
	bound.lease = lease
	bound.Hit = true
	bound.Entry = entry
	client := clientOccurrences(in.Request)
	if ShouldExpandPrefix(entry.Prefix, client, CompareLimits{}) {
		prefixMessages := MessagesFromOccurrences(entry.Prefix.Items)
		bound.Request.Context.Messages = append(prefixMessages, in.Request.Context.Messages...)
		bound.Expanded = true
	}
	bound.ReplayPrefixLength = len(entry.Prefix.Items)
	bound.Request.ReplayPrefixLength = bound.ReplayPrefixLength
	a.applySignatures(&bound, firstNonEmpty(in.Thread, previousID), in.Turn)
	return bound, nil
}

func (a *Authority) applySignatures(bound *Bound, thread string, turn *resourcebudget.Turn) {
	if a == nil || bound == nil || strings.TrimSpace(thread) == "" {
		return
	}
	for i := range bound.Request.Context.Messages {
		message := &bound.Request.Context.Messages[i]
		for j := range message.Content {
			part := &message.Content[j]
			if part.Type != protocol.ContentToolCall || strings.TrimSpace(part.ToolCallID) == "" {
				continue
			}
			owned, ok := a.LookupSignature(bound.Owner, thread, part.ToolCallID, turn)
			if !ok {
				if thoughtSignatureFromPart(*part) != "" {
					part.ThoughtSignature = ""
					if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil {
						part.ProviderMetadata.Google.ThoughtSignature = ""
					}
				}
				continue
			}
			part.ThoughtSignature = owned
			if part.ProviderMetadata == nil {
				part.ProviderMetadata = &protocol.ProviderOpaqueMetadata{}
			}
			if part.ProviderMetadata.Google == nil {
				part.ProviderMetadata.Google = &protocol.GoogleOpaqueMetadata{}
			}
			part.ProviderMetadata.Google.ThoughtSignature = owned
		}
	}
}

func (a *Authority) RememberProviderState(owner Owner, durable bool, thread string, payload json.RawMessage) error {
	if a == nil {
		return nil
	}
	thread = strings.TrimSpace(thread)
	if thread == "" || len(payload) == 0 || !json.Valid(payload) {
		return nil
	}
	entry := Entry{
		Key:     ReplayKey{Thread: thread, CallID: PrefixCallID, Owner: owner},
		Payload: append(json.RawMessage(nil), payload...),
		Prefix: Prefix{
			Items: []Occurrence{
				{Kind: "user", Payload: json.RawMessage(`{"text":""}`)},
				{Kind: "assistant", ID: "cursor-checkpoint", Payload: json.RawMessage(`{"text":""}`)},
			},
			ProviderOutputBoundary: 1,
		},
	}
	if err := a.store.Put(entry); err != nil {
		return err
	}
	if durable {
		return a.queuePersist()
	}
	return nil
}

func (a *Authority) Remember(owner Owner, durable bool, request protocol.ParsedRequest, response json.RawMessage) error {
	if a == nil {
		return nil
	}
	id, output, ok := parseCompletedResponse(response)
	if !ok {
		return nil
	}
	requestItems := OccurrencesFromMessages(request.Context.Messages)
	outputItems := OccurrencesFromOutput(output)
	if len(outputItems) == 0 {
		return nil
	}
	items := append(append([]Occurrence(nil), requestItems...), outputItems...)
	boundary := -1
	for index := len(requestItems); index < len(items); index++ {
		if strings.TrimSpace(items[index].ID) != "" || strings.TrimSpace(items[index].CallID) != "" {
			boundary = index
			break
		}
	}
	if boundary < 0 {
		return nil
	}
	entry := Entry{
		Key:    ReplayKey{Thread: id, CallID: PrefixCallID, Owner: owner},
		Prefix: Prefix{Items: items, ProviderOutputBoundary: boundary},
	}
	if err := a.store.Put(entry); err != nil {
		return err
	}
	for _, item := range outputItems {
		if item.Kind != "function_call" || strings.TrimSpace(item.CallID) == "" {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(item.Payload, &fields) != nil {
			continue
		}
		signature := rawJSONString(fields["thought_signature"])
		if signature == "" {
			continue
		}
		_ = a.RememberSignature(owner, durable, firstNonEmpty(request.PreviousResponseID, id), item.CallID, signature)
	}
	if durable {
		return a.queuePersist()
	}
	return nil
}

func (a *Authority) RememberOutputSignatures(owner Owner, durable bool, thread string, response json.RawMessage) error {
	if a == nil {
		return nil
	}
	_, output, ok := parseCompletedResponse(response)
	if !ok {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(output, &items) != nil {
		return nil
	}
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		if rawJSONString(item["type"]) != "function_call" {
			continue
		}
		callID := rawJSONString(item["call_id"])
		signature := outputThoughtSignature(item)
		if callID == "" || signature == "" {
			continue
		}
		if err := a.RememberSignature(owner, durable, thread, callID, signature); err != nil {
			return err
		}
	}
	return nil
}

func (a *Authority) RememberSignature(owner Owner, durable bool, thread, callID, signature string) error {
	if a == nil {
		return nil
	}
	thread = strings.TrimSpace(thread)
	callID = strings.TrimSpace(callID)
	signature = strings.TrimSpace(signature)
	if thread == "" || callID == "" || signature == "" || len(signature) > maxThoughtSignatureBytes {
		return nil
	}
	key := ReplayKey{Thread: thread, CallID: callID, Owner: owner}
	if current, ok := a.store.Peek(key, owner); ok {
		var existing struct {
			ThoughtSignature string `json:"thought_signature"`
		}
		if json.Unmarshal(current.Payload, &existing) == nil && existing.ThoughtSignature != "" && existing.ThoughtSignature != signature {
			return nil
		}
		if existing.ThoughtSignature == signature {
			return nil
		}
	}
	entry := Entry{
		Key:     key,
		Payload: compactJSON(map[string]any{"thought_signature": signature}),
		Prefix: Prefix{
			Items:                  []Occurrence{{Kind: "thought_signature", CallID: callID, Payload: compactJSON(map[string]any{"thought_signature": signature})}},
			ProviderOutputBoundary: 0,
		},
	}
	if err := a.store.Put(entry); err != nil {
		return err
	}
	if durable {
		return a.queuePersist()
	}
	return nil
}

func (a *Authority) LookupSignature(owner Owner, thread, callID string, turn *resourcebudget.Turn) (string, bool) {
	if a == nil || turn == nil {
		return "", false
	}
	thread = strings.TrimSpace(thread)
	callID = strings.TrimSpace(callID)
	if thread == "" || callID == "" {
		return "", false
	}
	lease, ok, err := a.store.Lease(ReplayKey{Thread: thread, CallID: callID, Owner: owner}, owner, turn)
	if err != nil || !ok {
		return "", false
	}
	defer lease.Release()
	var payload struct {
		ThoughtSignature string `json:"thought_signature"`
	}
	if json.Unmarshal(lease.Entry().Payload, &payload) != nil || payload.ThoughtSignature == "" {
		return "", false
	}
	return payload.ThoughtSignature, true
}

func (a *Authority) Settle(ctx context.Context) error {
	if a == nil || a.persister == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	wait := a.persistWait
	if wait <= 0 {
		wait = defaultPersistWait
	}
	limited, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	if err := a.persister.Flush(limited); err != nil {
		if limited.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

func (a *Authority) Close(ctx context.Context) error {
	if a == nil || a.persister == nil {
		return nil
	}
	return a.persister.Close(ctx)
}

func (a *Authority) queuePersist() error {
	if a == nil || a.persister == nil {
		return nil
	}
	return a.persister.Queue()
}

func parseCompletedResponse(raw json.RawMessage) (id string, output json.RawMessage, ok bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", nil, false
	}
	var response struct {
		ID                string          `json:"id"`
		Status            string          `json:"status"`
		Output            json.RawMessage `json:"output"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	if json.Unmarshal(trimmed, &response) != nil || strings.TrimSpace(response.ID) == "" {
		return "", nil, false
	}
	switch response.Status {
	case "", "completed":
	case "incomplete":
		if response.IncompleteDetails == nil || response.IncompleteDetails.Reason != "max_output_tokens" {
			return "", nil, false
		}
	default:
		return "", nil, false
	}
	if len(bytes.TrimSpace(response.Output)) == 0 {
		return "", nil, false
	}
	return response.ID, response.Output, true
}

func clientOccurrences(request protocol.ParsedRequest) []Occurrence {
	if items, ok := OccurrencesFromRequestInput(request.Raw); ok {
		return items
	}
	return OccurrencesFromMessages(request.Context.Messages)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
