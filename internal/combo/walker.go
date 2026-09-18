package combo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type Target struct {
	Member   Member
	Model    string
	Provider providers.Responses
}

type Attempt struct {
	Member   string
	Status   int
	Code     string
	Decision string
}

type Walker struct {
	Targets      []Target
	committedID  string
	hasCommitted bool
	attempts     []Attempt
}

type incompletePrecommitError struct {
	reason string
}

func (e *incompletePrecommitError) Error() string {
	reason := strings.TrimSpace(e.reason)
	if reason == "" {
		reason = "incomplete"
	}
	return fmt.Sprintf("combo member stream incomplete before model-visible output: %s", reason)
}

func retryableZeroOutputIncompleteReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "adapter_eof", "missing_terminal_event", "upstream_stall_timeout":
		return true
	default:
		return false
	}
}

func incompleteEventReason(event protocol.Event) string {
	if reason := strings.TrimSpace(event.StopReason); reason != "" {
		return reason
	}
	return strings.TrimSpace(event.Reason)
}

func (w *Walker) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if w == nil || len(w.Targets) == 0 {
		return nil, fmt.Errorf("combo requires at least one member")
	}
	members := make([]Member, len(w.Targets))
	for i, target := range w.Targets {
		members[i] = target.Member
		if members[i].ID == "" {
			members[i].ID = fmt.Sprintf("%d", i)
		}
	}
	parent := providers.CloneDispatch(dispatch)
	var stream providers.EventStream
	err := Walk(ctx, members, func(ctx context.Context, member Member, _ int) (Result, error) {
		target, ok := w.targetFor(member.ID)
		if !ok || target.Provider == nil {
			return Result{Status: 502, Message: "combo member provider is missing"}, nil
		}
		child := providers.CloneDispatch(parent)
		child.Parsed.UpstreamModelID = target.Model
		opened, openErr := target.Provider.Open(ctx, child)
		if openErr != nil {
			if errors.Is(openErr, context.Canceled) || errors.Is(openErr, context.DeadlineExceeded) {
				return Result{}, openErr
			}
			result := ClassifyOpenError(openErr)
			w.noteAttempt(member.ID, result)
			return result, nil
		}
		prefixed, commitErr := commitOpenedStream(opened)
		if commitErr != nil {
			_ = opened.Close()
			if errors.Is(commitErr, context.Canceled) || errors.Is(commitErr, context.DeadlineExceeded) {
				return Result{}, commitErr
			}
			result := ClassifyOpenError(commitErr)
			w.noteAttempt(member.ID, result)
			return result, nil
		}
		stream = prefixed
		w.committedID = member.ID
		w.hasCommitted = true
		w.attempts = append(w.attempts, Attempt{Member: member.ID, Status: 200, Decision: DecisionCommitted})
		return Result{Status: 200}, nil
	})
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, fmt.Errorf("combo exhausted all members")
	}
	return stream, nil
}

func (w *Walker) Protocol() string {
	if w == nil {
		return ""
	}
	target, ok := w.targetFor(w.committedID)
	if !ok && len(w.Targets) == 1 {
		target = w.Targets[0]
		ok = true
	}
	if !ok || target.Provider == nil {
		return strings.TrimSpace(target.Member.Protocol)
	}
	if reporter, ok := target.Provider.(interface{ Protocol() string }); ok {
		if proto := strings.TrimSpace(reporter.Protocol()); proto != "" {
			return proto
		}
	}
	return strings.TrimSpace(target.Member.Protocol)
}

func (w *Walker) CommittedID() string {
	if w == nil {
		return ""
	}
	return w.committedID
}

func (w *Walker) Attempts() []Attempt {
	if w == nil || len(w.attempts) == 0 {
		return nil
	}
	out := make([]Attempt, len(w.attempts))
	copy(out, w.attempts)
	return out
}

func (w *Walker) noteAttempt(memberID string, result Result) {
	if w == nil {
		return
	}
	decision := FailureDecision(result.Status, result.Message, result.Code)
	w.attempts = append(w.attempts, Attempt{
		Member:   memberID,
		Status:   result.Status,
		Code:     result.Code,
		Decision: decision,
	})
}

// OpenCommitted reopens the member that already produced model-visible output.
// Hidden recovery continuations must use this so combo failover cannot hop mid-turn.
func (w *Walker) OpenCommitted(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if w == nil || !w.hasCommitted {
		return nil, fmt.Errorf("combo has no committed member")
	}
	target, ok := w.targetFor(w.committedID)
	if !ok || target.Provider == nil {
		return nil, fmt.Errorf("combo committed member provider is missing")
	}
	child := providers.CloneDispatch(dispatch)
	child.Parsed.UpstreamModelID = target.Model
	return target.Provider.Open(ctx, child)
}

func (w *Walker) targetFor(id string) (Target, bool) {
	for i, target := range w.Targets {
		memberID := target.Member.ID
		if memberID == "" {
			memberID = fmt.Sprintf("%d", i)
		}
		if memberID == id {
			return target, true
		}
	}
	return Target{}, false
}

func ClassifyOpenError(err error) Result {
	if err == nil {
		return Result{Status: 200}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Result{Status: 499, Message: err.Error()}
	}
	var incompleteErr *incompletePrecommitError
	if errors.As(err, &incompleteErr) {
		return Result{Status: 502, Message: incompleteErr.Error(), Code: CodeIncompletePrecommitFailover}
	}
	message := err.Error()
	status := httpStatusFromError(message)
	if status == 0 {
		status = 502
	}
	return Result{Status: status, Message: message, Code: classifyCode(status, message)}
}

func httpStatusFromError(message string) int {
	idx := strings.LastIndex(message, "HTTP ")
	if idx < 0 {
		return 0
	}
	n := 0
	digits := 0
	for _, r := range message[idx+5:] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		digits++
		if digits == 3 {
			break
		}
	}
	if digits != 3 || n < 100 || n > 599 {
		return 0
	}
	return n
}

func commitOpenedStream(opened providers.EventStream) (providers.EventStream, error) {
	if opened == nil {
		return nil, fmt.Errorf("combo member stream is missing")
	}
	var prefix []protocol.Event
	for {
		event, err := opened.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("combo member stream ended before model-visible output")
			}
			return nil, err
		}
		if event.Type == protocol.EventError {
			return nil, streamFailureError(event)
		}
		if event.Type == protocol.EventIncomplete {
			reason := incompleteEventReason(event)
			if retryableZeroOutputIncompleteReason(reason) {
				return nil, &incompletePrecommitError{reason: reason}
			}
			prefix = append(prefix, event)
			return &prefixedStream{inner: opened, prefix: prefix}, nil
		}
		prefix = append(prefix, event)
		if comboEventIsVisible(event) {
			return &prefixedStream{inner: opened, prefix: prefix}, nil
		}
	}
}

func streamFailureError(event protocol.Event) error {
	message := strings.TrimSpace(event.Message)
	if message == "" {
		message = "combo member stream error"
	}
	if event.HTTPStatus >= 100 && event.HTTPStatus <= 599 {
		return fmt.Errorf("%s HTTP %d", message, event.HTTPStatus)
	}
	return fmt.Errorf("%s", message)
}

func comboEventIsVisible(event protocol.Event) bool {
	switch event.Type {
	case protocol.EventTextDelta,
		protocol.EventThinkingDelta,
		protocol.EventThinkingSignature,
		protocol.EventRedactedThinking,
		protocol.EventKiroRedactedReasoning,
		protocol.EventReasoningRawDelta,
		protocol.EventToolCallStart,
		protocol.EventToolCallDelta,
		protocol.EventToolCallEnd,
		protocol.EventAssistantBoundary,
		protocol.EventWebSearchCallBegin,
		protocol.EventWebSearchCallEnd,
		protocol.EventCompaction,
		protocol.EventDone:
		return true
	default:
		return false
	}
}

type prefixedStream struct {
	inner  providers.EventStream
	prefix []protocol.Event
}

func (s *prefixedStream) Next() (protocol.Event, error) {
	if len(s.prefix) > 0 {
		event := s.prefix[0]
		s.prefix = s.prefix[1:]
		return event, nil
	}
	return s.inner.Next()
}

func (s *prefixedStream) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

// PhysicalOwnership forwards the committed member's pin. prefixedStream is
// identity-transparent (prefix buffering only); dropping this would make
// runModelTurn capture nil after combo commit.
func (s *prefixedStream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providers.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}
