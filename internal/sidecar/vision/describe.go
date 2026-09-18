package vision

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/requestpolicy"
	"github.com/Wibias/Benes/internal/sidecar"
)

var (
	ErrTooManyImages = errors.New("vision sidecar image count exceeds the bound")
	ErrImageTooLarge = errors.New("vision sidecar image exceeds the byte bound")
	ErrBadImage      = errors.New("vision sidecar image is not a bounded data URL")
	ErrEmptyResult   = errors.New("vision sidecar returned no description")
)

const (
	DefaultMaxImages      = 4
	DefaultMaxImageBytes  = 8 << 20
	DefaultMaxDescription = 4000
)

type Request struct {
	Images []string
}

type Result struct {
	ProviderID  string
	ModelID     string
	Description string
}

type Client struct {
	open           providers.Responses
	maxImages      int
	maxImageBytes  int
	maxDescription int
}

func New(open providers.Responses, maxImages, maxImageBytes, maxDescription int) *Client {
	if maxImages <= 0 {
		maxImages = DefaultMaxImages
	}
	if maxImageBytes <= 0 {
		maxImageBytes = DefaultMaxImageBytes
	}
	if maxDescription <= 0 {
		maxDescription = DefaultMaxDescription
	}
	return &Client{open: open, maxImages: maxImages, maxImageBytes: maxImageBytes, maxDescription: maxDescription}
}

func (c *Client) Describe(ctx context.Context, candidate sidecar.Candidate, req Request) (Result, error) {
	if c == nil || c.open == nil {
		return Result{}, sidecar.ErrUnsupportedBackend
	}
	if !candidate.Proven || !candidate.ImageInput || candidate.Modality != sidecar.ModalityVisionDescribe {
		return Result{}, sidecar.ErrUnprovenCapability
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	images, err := boundImages(req.Images, c.maxImages, c.maxImageBytes)
	if err != nil {
		return Result{}, err
	}
	content := []protocol.ContentPart{{Type: protocol.ContentText, Text: "Describe the image. Reply with plain text only."}}
	for _, image := range images {
		content = append(content, protocol.ContentPart{Type: protocol.ContentImage, ImageURL: image})
	}
	stream, err := c.open.Open(ctx, providers.DispatchRequest{
		// A sidecar call is its own logical request: freeze the configured policy here
		// instead of leaving the provider boundary to read live settings.
		ConfiguredServiceTier: requestpolicy.AdmittedServiceTier(),
		Parsed: protocol.ParsedRequest{
			Source:          protocol.RequestSourceChatCompletions,
			ModelID:         candidate.ModelID,
			UpstreamModelID: candidate.ModelID,
			Stream:          true,
			Context: protocol.Context{
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: content}},
			},
		},
	})
	if err != nil {
		return Result{}, err
	}
	defer stream.Close()
	var b strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		ev, nextErr := stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return Result{}, nextErr
		}
		if ev.Type == protocol.EventDone {
			break
		}
		if ev.Type == protocol.EventError {
			if ev.HTTPStatus == 429 {
				return Result{}, fmt.Errorf("vision sidecar quota: %s", strings.TrimSpace(ev.Message))
			}
			return Result{}, fmt.Errorf("vision sidecar failed: %s", strings.TrimSpace(ev.Message))
		}
		if ev.Type == protocol.EventTextDelta {
			b.WriteString(ev.Text)
			if utf8.RuneCountInString(b.String()) > c.maxDescription {
				break
			}
		}
	}
	text := sidecar.BoundText(strings.TrimSpace(b.String()), c.maxDescription)
	if text == "" {
		return Result{}, ErrEmptyResult
	}
	return Result{ProviderID: candidate.ProviderID, ModelID: candidate.ModelID, Description: text}, nil
}

func boundImages(images []string, maxCount, maxBytes int) ([]string, error) {
	if len(images) == 0 {
		return nil, ErrBadImage
	}
	if len(images) > maxCount {
		return nil, ErrTooManyImages
	}
	out := make([]string, 0, len(images))
	for _, raw := range images {
		image := strings.TrimSpace(raw)
		prefix, payload, ok := strings.Cut(image, ",")
		if !ok || !strings.HasPrefix(strings.ToLower(prefix), "data:image/") || !strings.Contains(strings.ToLower(prefix), ";base64") {
			return nil, ErrBadImage
		}
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, ErrBadImage
		}
		if len(decoded) > maxBytes {
			return nil, ErrImageTooLarge
		}
		out = append(out, image)
	}
	return out, nil
}
