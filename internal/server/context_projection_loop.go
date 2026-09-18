package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/contextprojection"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

type committedOpener interface {
	OpenCommitted(context.Context, providercontract.DispatchRequest) (EventStream, error)
}

type recoveryLoop struct {
	ctx        context.Context
	provider   Provider
	req        providercontract.DispatchRequest
	session    *contextprojection.RecoverySession
	canonical  protocol.Context
	inner      EventStream
	pending    []protocol.Event
	hiding     bool
	hideID     string
	args       strings.Builder
	rounds     int
	failOpened bool
}

func openRecoveryLoop(ctx context.Context, provider Provider, req providercontract.DispatchRequest, proj providerProjection) (EventStream, error) {
	if provider == nil {
		return nil, io.EOF
	}
	if ctx == nil {
		ctx = context.Background()
	}
	inner, err := provider.Open(ctx, req)
	if err != nil {
		return nil, err
	}
	if !proj.active || proj.registry == nil {
		return inner, nil
	}
	return &recoveryLoop{
		ctx:       ctx,
		provider:  provider,
		req:       req,
		session:   contextprojection.NewRecoverySession(proj.registry, ctx),
		canonical: proj.canonical,
		inner:     inner,
	}, nil
}

func (s *recoveryLoop) Next() (protocol.Event, error) {
	if s == nil {
		return protocol.Event{}, io.EOF
	}
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}
	if s.inner == nil {
		return protocol.Event{}, io.EOF
	}
	for {
		ev, err := s.inner.Next()
		if err != nil {
			if err == io.EOF && s.hiding {
				return s.failOpenOrError("recovery tool stream ended before arguments arrived")
			}
			return ev, err
		}
		if s.hiding {
			if ev.ID != "" && ev.ID != s.hideID && (ev.Type == protocol.EventToolCallStart || ev.Type == protocol.EventDone) {
				s.hiding = false
				return s.failOpenOrError("recovery tool call was interrupted")
			}
			if ev.Type == protocol.EventToolCallDelta {
				s.args.WriteString(ev.Arguments)
				continue
			}
			if ev.Type == protocol.EventToolCallEnd {
				if strings.TrimSpace(ev.Arguments) != "" {
					s.args.Reset()
					s.args.WriteString(ev.Arguments)
				}
				s.hiding = false
				return s.continueAfterCall()
			}
			if ev.Type == protocol.EventDone || ev.Type == protocol.EventError {
				s.hiding = false
				return s.failOpenOrError("recovery tool call did not complete")
			}
			continue
		}
		if isRecoveryToolEvent(ev) {
			s.hiding = true
			s.hideID = ev.ID
			s.args.Reset()
			if ev.Type == protocol.EventToolCallEnd {
				s.args.WriteString(ev.Arguments)
				s.hiding = false
				return s.continueAfterCall()
			}
			if ev.Type == protocol.EventToolCallDelta {
				s.args.WriteString(ev.Arguments)
			}
			continue
		}
		return ev, nil
	}
}

func (s *recoveryLoop) continueAfterCall() (protocol.Event, error) {
	if s.rounds+1 >= contextprojection.MaxInternalModelRoundsPerTurn {
		return protocol.Event{Type: protocol.EventError, Message: "context recovery exceeded the hidden-loop budget", HTTPStatus: 502, ErrorType: "upstream_error"}, nil
	}
	payload, failOpen := s.session.HandleCall(s.args.String())
	s.rounds++
	if failOpen && !s.failOpened {
		s.failOpened = true
		return s.reopen(true, "", "")
	}
	return s.reopen(false, s.hideID, payload)
}

func (s *recoveryLoop) failOpenOrError(message string) (protocol.Event, error) {
	if !s.failOpened {
		s.failOpened = true
		return s.reopen(true, "", "")
	}
	return protocol.Event{Type: protocol.EventError, Message: message, HTTPStatus: 502, ErrorType: "upstream_error"}, nil
}

func (s *recoveryLoop) reopen(restoreCanonical bool, callID, payload string) (protocol.Event, error) {
	// Defensive physical continuity: Fabric primary opens unpinned, then hidden
	// recovery may replace s.inner. Capture CURRENT inner ownership before Close
	// and clone it into the reopen dispatch so Google/OpenAI Responses cannot
	// Select a different key while runModelTurn still holds the pre-Next sample.
	currentPin := s.PhysicalOwnership()
	_ = s.inner.Close()
	next := providercontract.CloneDispatch(s.req)
	if currentPin != nil {
		pin := *currentPin
		next.PhysicalPin = &pin
	}
	if restoreCanonical {
		next.Parsed.Context = s.canonical
	} else {
		msgs := append([]protocol.Message(nil), next.Parsed.Context.Messages...)
		msgs = append(msgs,
			protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
				Type: protocol.ContentToolCall, ToolCallID: callID, ToolName: contextprojection.RecoveryToolName,
			}}},
			protocol.Message{Role: protocol.RoleToolResult, ToolCallID: callID, ToolName: contextprojection.RecoveryToolName, Content: []protocol.ContentPart{{
				Type: protocol.ContentText, Text: payload,
			}}},
		)
		next.Parsed.Context.Messages = msgs
	}
	if next.Turn != nil {
		encoded, err := json.Marshal(next.Parsed.Context.Messages)
		if err != nil {
			s.inner = nil
			return protocol.Event{Type: protocol.EventError, Message: "context recovery continuation failed", HTTPStatus: 502, ErrorType: "upstream_error"}, nil
		}
		if _, err := next.Turn.Reserve(resourcebudget.ClassContinuation, int64(len(encoded))); err != nil {
			s.inner = nil
			return protocol.Event{Type: protocol.EventError, Message: "request resource budget is exhausted", HTTPStatus: 503, ErrorType: "server_error", Code: "resource_exhausted"}, nil
		}
	}
	s.req = next
	inner, err := reopenProvider(s.ctx, s.provider, next)
	if err != nil {
		s.inner = nil
		return protocol.Event{Type: protocol.EventError, Message: "context recovery continuation failed", HTTPStatus: 502, ErrorType: "upstream_error"}, nil
	}
	s.inner = inner
	return s.Next()
}

func reopenProvider(ctx context.Context, provider Provider, req providercontract.DispatchRequest) (EventStream, error) {
	if committed, ok := provider.(committedOpener); ok {
		return committed.OpenCommitted(ctx, req)
	}
	return provider.Open(ctx, req)
}

func isRecoveryToolEvent(ev protocol.Event) bool {
	switch ev.Type {
	case protocol.EventToolCallStart, protocol.EventToolCallDelta, protocol.EventToolCallEnd:
		return strings.TrimSpace(ev.Name) == contextprojection.RecoveryToolName
	default:
		return false
	}
}

func (s *recoveryLoop) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

// PhysicalOwnership forwards the current inner pin. Recovery looping is identity-transparent.
func (s *recoveryLoop) PhysicalOwnership() *providercontract.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providercontract.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}
