package server

import (
	"context"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/contextprojection"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
)

func TestOpenRecoveryLoopHidesRecoveryToolAndReopensCommittedProvider(t *testing.T) {
	registry := contextprojection.NewRegistry()
	artifact := registry.Register(
		contextprojection.Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec"},
		0,
		"omitted-body",
		len("omitted-body"),
		1,
		false,
	)
	opens := 0
	var secondMsgs []protocol.Message
	provider := providerFunc(func(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
		opens++
		if opens == 1 {
			return &sliceStream{events: []protocol.Event{
				{Type: protocol.EventToolCallStart, ID: "rec-1", Name: contextprojection.RecoveryToolName},
				{Type: protocol.EventToolCallEnd, ID: "rec-1", Name: contextprojection.RecoveryToolName, Arguments: `{"op":"read","ref":"` + artifact.Ref + `"}`},
				{Type: protocol.EventDone},
			}}, nil
		}
		secondMsgs = append([]protocol.Message(nil), req.Parsed.Context.Messages...)
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "recovered"},
			{Type: protocol.EventDone},
		}}, nil
	})
	stream, err := openRecoveryLoop(context.Background(), provider, providercontract.DispatchRequest{
		Parsed: protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}}},
	}, providerProjection{registry: registry, active: true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var texts []string
	var leaked bool
	for {
		ev, nextErr := stream.Next()
		if nextErr != nil {
			break
		}
		if ev.Name == contextprojection.RecoveryToolName {
			leaked = true
		}
		if ev.Type == protocol.EventTextDelta {
			texts = append(texts, ev.Text)
		}
	}
	if leaked {
		t.Fatal("recovery tool leaked to the client stream")
	}
	if opens != 2 {
		t.Fatalf("opens=%d", opens)
	}
	if strings.Join(texts, "") != "recovered" {
		t.Fatalf("texts=%v", texts)
	}
	found := false
	for _, msg := range secondMsgs {
		if msg.Role == protocol.RoleToolResult && strings.Contains(messageText(msg), "omitted-body") {
			found = true
		}
	}
	if !found {
		t.Fatalf("continuation missing exact recovery text: %#v", secondMsgs)
	}
}

func messageText(msg protocol.Message) string {
	var b strings.Builder
	for _, part := range msg.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

type pinnedRecoveryStream struct {
	sliceStream
	pin *providercontract.PhysicalPin
}

func (s *pinnedRecoveryStream) PhysicalOwnership() *providercontract.PhysicalPin {
	if s == nil || s.pin == nil {
		return nil
	}
	pin := *s.pin
	return &pin
}

// TestRecoveryLoopHiddenReopenPreservesPhysicalPin proves Fabric-primary-shaped
// recovery (PhysicalPin unset on the initial dispatch) cannot reopen unpinned:
// a counterfactual unpinned reopen would select K2 while runModelTurn still holds
// the pre-Next PhysicalOwnership sample from K1.
func TestRecoveryLoopHiddenReopenPreservesPhysicalPin(t *testing.T) {
	registry := contextprojection.NewRegistry()
	artifact := registry.Register(
		contextprojection.Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec"},
		0,
		"omitted-body",
		len("omitted-body"),
		1,
		false,
	)

	var (
		opens        int
		reopenPinNil bool
		reopenRef    string
		finalRef     string
	)
	provider := providerFunc(func(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
		opens++
		if opens == 1 {
			return &pinnedRecoveryStream{
				sliceStream: sliceStream{events: []protocol.Event{
					{Type: protocol.EventToolCallStart, ID: "rec-1", Name: contextprojection.RecoveryToolName},
					{Type: protocol.EventToolCallEnd, ID: "rec-1", Name: contextprojection.RecoveryToolName, Arguments: `{"op":"read","ref":"` + artifact.Ref + `"}`},
					{Type: protocol.EventDone},
				}},
				pin: &providercontract.PhysicalPin{Destination: "google:aistudio", CredentialRef: "K1", AuthClass: "api_key"},
			}, nil
		}
		// Counterfactual: unpinned reopen selects K2; pinned reopen stays on K1.
		ref := "K2"
		if req.PhysicalPin != nil && strings.TrimSpace(req.PhysicalPin.CredentialRef) != "" {
			ref = strings.TrimSpace(req.PhysicalPin.CredentialRef)
		} else {
			reopenPinNil = true
		}
		reopenRef = ref
		finalRef = ref
		return &pinnedRecoveryStream{
			sliceStream: sliceStream{events: []protocol.Event{
				{Type: protocol.EventTextDelta, Text: "native-from-" + ref},
				{Type: protocol.EventDone},
			}},
			pin: &providercontract.PhysicalPin{Destination: "google:aistudio", CredentialRef: ref, AuthClass: "api_key"},
		}, nil
	})

	stream, err := openRecoveryLoop(context.Background(), provider, providercontract.DispatchRequest{
		Parsed: protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}}},
	}, providerProjection{
		registry:  registry,
		active:    true,
		canonical: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Same capture shape as runModelTurnImpl: sample BEFORE Next().
	var streamPhysical *providercontract.PhysicalPin
	if reporter, ok := stream.(interface{ PhysicalOwnership() *providercontract.PhysicalPin }); ok {
		streamPhysical = reporter.PhysicalOwnership()
	}
	if streamPhysical == nil || streamPhysical.CredentialRef != "K1" {
		t.Fatalf("pre-sample pin=%#v want K1", streamPhysical)
	}

	var visible string
	for {
		ev, nextErr := stream.Next()
		if nextErr != nil {
			break
		}
		if ev.Type == protocol.EventTextDelta {
			visible += ev.Text
		}
	}
	if opens != 2 {
		t.Fatalf("opens=%d want 2 (hidden recovery reopen)", opens)
	}

	returned := *streamPhysical
	live := stream.(interface{ PhysicalOwnership() *providercontract.PhysicalPin }).PhysicalOwnership()

	if reopenPinNil || reopenRef != "K1" {
		t.Fatalf("hidden reopen was unpinned/drifted: pinNil=%v reopenRef=%q finalRef=%q visible=%q returnedPin=%q livePin=%v",
			reopenPinNil, reopenRef, finalRef, visible, returned.CredentialRef, live)
	}
	if visible != "native-from-K1" {
		t.Fatalf("model-visible continuation=%q want native-from-K1", visible)
	}
	if returned.CredentialRef != "K1" {
		t.Fatalf("runModelTurn-shaped returned pin=%q want K1", returned.CredentialRef)
	}
	if live == nil || live.CredentialRef != "K1" {
		t.Fatalf("live PhysicalOwnership after recovery=%#v want K1", live)
	}
}
