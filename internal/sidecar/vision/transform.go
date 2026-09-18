package vision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/sidecar"
)

type Describer interface {
	Describe(context.Context, sidecar.Candidate, Request) (Result, error)
}

type TransformOptions struct {
	MaxImages      int
	MaxImageBytes  int
	MaxDescription int
	Timeout        time.Duration
}

type imagePosition struct {
	message int
	part    int
	url     string
}

func TransformRequest(ctx context.Context, in protocol.ParsedRequest, candidate sidecar.Candidate, describer Describer, opts TransformOptions) (protocol.ParsedRequest, error) {
	positions := collectImagePositions(in)
	if len(positions) == 0 {
		return protocol.CloneParsedRequest(in), nil
	}
	if describer == nil {
		return protocol.ParsedRequest{}, sidecar.ErrUnsupportedBackend
	}

	maxImages := opts.MaxImages
	if maxImages <= 0 {
		maxImages = DefaultMaxImages
	}
	if len(positions) > maxImages {
		return protocol.ParsedRequest{}, ErrTooManyImages
	}
	maxImageBytes := opts.MaxImageBytes
	if maxImageBytes <= 0 {
		maxImageBytes = DefaultMaxImageBytes
	}
	for _, position := range positions {
		if _, err := boundImages([]string{position.url}, 1, maxImageBytes); err != nil {
			return protocol.ParsedRequest{}, err
		}
	}
	if !candidate.Proven || candidate.Modality != sidecar.ModalityVisionDescribe || candidate.Class != sidecar.ClassVisionDescribe || !candidate.ImageInput {
		return protocol.ParsedRequest{}, sidecar.ErrUnprovenCapability
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return protocol.ParsedRequest{}, err
	}

	maxDescription := opts.MaxDescription
	if maxDescription <= 0 {
		maxDescription = DefaultMaxDescription
	}
	descriptions := make([]string, len(positions))
	for i, position := range positions {
		result, err := describer.Describe(ctx, candidate, Request{Images: []string{position.url}})
		if err != nil {
			return protocol.ParsedRequest{}, err
		}
		text := strings.TrimSpace(sidecar.BoundText(result.Description, maxDescription))
		if text == "" {
			return protocol.ParsedRequest{}, ErrEmptyResult
		}
		descriptions[i] = text
	}

	out := protocol.CloneParsedRequest(in)
	for i, position := range positions {
		out.Context.Messages[position.message].Content[position.part] = protocol.ContentPart{
			Type: protocol.ContentText,
			Text: formatImageDescription(descriptions[i]),
		}
	}
	return out, nil
}

func collectImagePositions(in protocol.ParsedRequest) []imagePosition {
	var positions []imagePosition
	for messageIndex, message := range in.Context.Messages {
		for partIndex, part := range message.Content {
			if part.Type != protocol.ContentImage {
				continue
			}
			positions = append(positions, imagePosition{
				message: messageIndex,
				part:    partIndex,
				url:     part.ImageURL,
			})
		}
	}
	return positions
}

func formatImageDescription(description string) string {
	return fmt.Sprintf("[Benes image description]\n%s\n[/Benes image description]", strings.TrimSpace(description))
}
