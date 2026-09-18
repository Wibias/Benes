package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/timeline"
)

func WriteSSE(ctx context.Context, writer http.ResponseWriter, stream providers.EventStream, model string, options Options) error {
	if ctx == nil {
		return fmt.Errorf("chat SSE context is required")
	}
	if writer == nil {
		return fmt.Errorf("chat SSE response writer is required")
	}
	if stream == nil {
		return fmt.Errorf("chat SSE provider stream is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	encoder, err := New(model, options)
	if err != nil {
		return err
	}

	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)
	firstDownstream := false
	firstText := false

	for {
		if err := ctx.Err(); err != nil {
			markTrace(options.Trace, timeline.StageClientCancel, timeline.SideClient, "", false, "client_cancel")
			return err
		}
		event, nextErr := stream.Next()
		if nextErr != nil {
			switch {
			case errors.Is(nextErr, io.EOF):
				event = protocol.Event{Type: protocol.EventIncomplete, Reason: "adapter_eof"}
			default:
				event = protocol.Event{
					Type:       protocol.EventError,
					Message:    "provider stream failed",
					HTTPStatus: http.StatusBadGateway,
					ErrorType:  "upstream_error",
				}
			}
		}

		frames, handleErr := encoder.Handle(event)
		if handleErr != nil {
			markTrace(options.Trace, timeline.StageRelayTransform, timeline.SideRelay, "", false, "translator")
			return handleErr
		}
		if event.Validate() != nil {
			markTrace(options.Trace, timeline.StageRelayTransform, timeline.SideRelay, "", false, "malformed_frame")
		}
		terminal, writeErr := writeSSEFrames(writer, flusher, frames, options.Turn)
		if writeErr != nil {
			markTrace(options.Trace, timeline.StageDownstreamWrite, timeline.SideDownstream, "", false, "downstream_write")
			return writeErr
		}
		if !firstDownstream && len(frames) > 0 {
			firstDownstream = true
			markTrace(options.Trace, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneFirstDownstream, true, "")
		}
		if !firstText && event.Type == protocol.EventTextDelta && event.Text != "" {
			firstText = true
			markTrace(options.Trace, timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneTTFT, true, "")
		}
		if terminal {
			markTrace(options.Trace, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneDownstreamEnd, true, "")
			return nil
		}
	}
}

func Collect(stream providers.EventStream, model string, options Options) (map[string]any, error) {
	if stream == nil {
		return nil, fmt.Errorf("chat collector provider stream is required")
	}
	encoder, err := New(model, options)
	if err != nil {
		return nil, err
	}

	for {
		event, nextErr := stream.Next()
		if nextErr != nil {
			switch {
			case errors.Is(nextErr, io.EOF):
				event = protocol.Event{Type: protocol.EventIncomplete, Reason: "adapter_eof"}
			default:
				event = protocol.Event{
					Type:       protocol.EventError,
					Message:    "provider stream failed",
					HTTPStatus: http.StatusBadGateway,
					ErrorType:  "upstream_error",
				}
			}
		}

		frames, handleErr := encoder.Handle(event)
		if handleErr != nil {
			return nil, handleErr
		}
		if framesTerminal(frames) {
			return encoder.Completion()
		}
	}
}

func markTrace(tr *timeline.Trace, stage timeline.Stage, side timeline.Side, milestone timeline.Milestone, ok bool, cause string) {
	if tr == nil {
		return
	}
	tr.Mark(stage, side, milestone, ok, cause)
}

func reserveDownstream(turn *resourcebudget.Turn, n int) error {
	if turn == nil || n <= 0 {
		return nil
	}
	_, err := turn.Reserve(resourcebudget.ClassDownstreamQueue, int64(n))
	return err
}

func writeSSEFrames(writer io.Writer, flusher http.Flusher, frames []Frame, turn *resourcebudget.Turn) (bool, error) {
	terminal := false
	for _, frame := range frames {
		if frame.Done {
			if err := reserveDownstream(turn, len("data: [DONE]\n\n")); err != nil {
				return false, err
			}
			if _, err := io.WriteString(writer, "data: [DONE]\n\n"); err != nil {
				return false, err
			}
			turn.MarkCommitted()
			terminal = true

		} else {
			encoded, err := json.Marshal(frame.Payload)
			if err != nil {
				return false, err
			}
			if err := reserveDownstream(turn, len(encoded)+len("data: \n\n")); err != nil {
				return false, err
			}
			if err := sse.WriteJSON(writer, frame.Payload); err != nil {
				return false, err
			}
			turn.MarkCommitted()

			if _, failed := frame.Payload["error"]; failed {
				terminal = true
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return terminal, nil
}

func framesTerminal(frames []Frame) bool {
	for _, frame := range frames {
		if frame.Done {
			return true
		}
		if _, failed := frame.Payload["error"]; failed {
			return true
		}
	}
	return false
}
