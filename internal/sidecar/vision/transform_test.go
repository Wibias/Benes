package vision

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/sidecar"
)

type transformDescriberFunc func(context.Context, sidecar.Candidate, Request) (Result, error)

func (f transformDescriberFunc) Describe(ctx context.Context, candidate sidecar.Candidate, req Request) (Result, error) {
	return f(ctx, candidate, req)
}

func testVisionCandidate() sidecar.Candidate {
	return sidecar.Candidate{
		Modality:   sidecar.ModalityVisionDescribe,
		Class:      sidecar.ClassVisionDescribe,
		ProviderID: "vision-provider",
		ModelID:    "vision-model",
		ImageInput: true,
		Proven:     true,
	}
}

func TestTransformRequestWithoutImagesReturnsIndependentClone(t *testing.T) {
	in := protocol.ParsedRequest{
		ModelID: "p/m",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello"}},
		}}},
	}
	calls := 0
	out, err := TransformRequest(context.Background(), in, testVisionCandidate(), transformDescriberFunc(func(context.Context, sidecar.Candidate, Request) (Result, error) {
		calls++
		return Result{}, nil
	}), TransformOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("describer calls=%d", calls)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("out=%#v in=%#v", out, in)
	}
	out.Context.Messages[0].Content[0].Text = "changed"
	if in.Context.Messages[0].Content[0].Text != "hello" {
		t.Fatal("transform result aliases original request")
	}
}

func TestTransformRequestReplacesImagesInPlaceAfterAllDescriptionsSucceed(t *testing.T) {
	imageA := "data:image/png;base64,YQ=="
	imageB := "data:image/jpeg;base64,Yg=="
	in := protocol.ParsedRequest{
		ModelID: "p/m",
		Context: protocol.Context{Messages: []protocol.Message{
			{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{
					{Type: protocol.ContentText, Text: "before"},
					{Type: protocol.ContentImage, ImageURL: imageA, Detail: "high"},
					{Type: protocol.ContentText, Text: "between"},
					{Type: protocol.ContentImage, ImageURL: imageB},
					{Type: protocol.ContentText, Text: "after"},
				},
			},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "assistant"}}},
		}},
	}
	original := protocol.CloneParsedRequest(in)
	var seen []string
	describer := transformDescriberFunc(func(_ context.Context, _ sidecar.Candidate, req Request) (Result, error) {
		if len(req.Images) != 1 {
			t.Fatalf("Describe received %d images", len(req.Images))
		}
		seen = append(seen, req.Images[0])
		if len(seen) == 1 {
			return Result{Description: "first image"}, nil
		}
		return Result{Description: "second image"}, nil
	})

	out, err := TransformRequest(context.Background(), in, testVisionCandidate(), describer, TransformOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, original) {
		t.Fatal("original request was mutated")
	}
	if !reflect.DeepEqual(seen, []string{imageA, imageB}) {
		t.Fatalf("describe order=%#v", seen)
	}
	parts := out.Context.Messages[0].Content
	if len(parts) != 5 {
		t.Fatalf("parts=%#v", parts)
	}
	if parts[0].Type != protocol.ContentText || parts[0].Text != "before" ||
		parts[2].Type != protocol.ContentText || parts[2].Text != "between" ||
		parts[4].Type != protocol.ContentText || parts[4].Text != "after" {
		t.Fatalf("non-image order changed: %#v", parts)
	}
	if parts[1].Type != protocol.ContentText || parts[1].ImageURL != "" || parts[1].Text != "[Benes image description]\nfirst image\n[/Benes image description]" {
		t.Fatalf("first replacement=%#v", parts[1])
	}
	if parts[3].Type != protocol.ContentText || parts[3].ImageURL != "" || parts[3].Text != "[Benes image description]\nsecond image\n[/Benes image description]" {
		t.Fatalf("second replacement=%#v", parts[3])
	}
	if out.Context.Messages[1].Role != protocol.RoleAssistant || out.Context.Messages[1].Content[0].Text != "assistant" {
		t.Fatalf("message roles/content changed: %#v", out.Context.Messages)
	}
	for _, message := range out.Context.Messages {
		for _, part := range message.Content {
			if part.Type == protocol.ContentImage || part.ImageURL != "" {
				t.Fatalf("raw image remained after transform: %#v", part)
			}
		}
	}
}

func TestTransformRequestIsAllOrNothing(t *testing.T) {
	in := protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{
		Role: protocol.RoleUser,
		Content: []protocol.ContentPart{
			{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,YQ=="},
			{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,Yg=="},
		},
	}}}}
	original := protocol.CloneParsedRequest(in)
	calls := 0
	wantErr := errors.New("describe failed")
	describer := transformDescriberFunc(func(context.Context, sidecar.Candidate, Request) (Result, error) {
		calls++
		if calls == 2 {
			return Result{}, wantErr
		}
		return Result{Description: "first"}, nil
	})
	out, err := TransformRequest(context.Background(), in, testVisionCandidate(), describer, TransformOptions{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(out, protocol.ParsedRequest{}) {
		t.Fatalf("partial transformed request escaped: %#v", out)
	}
	if !reflect.DeepEqual(in, original) {
		t.Fatal("original request was mutated after transform failure")
	}
}

func TestTransformRequestRejectsUnboundedOrRemoteImagesBeforeDescribe(t *testing.T) {
	cases := []struct {
		name string
		parts []protocol.ContentPart
		opts TransformOptions
		want error
	}{
		{
			name: "too many",
			parts: []protocol.ContentPart{
				{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,YQ=="},
				{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,Yg=="},
			},
			opts: TransformOptions{MaxImages: 1},
			want: ErrTooManyImages,
		},
		{
			name: "remote URL",
			parts: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "https://example.com/image.png"}},
			want: ErrBadImage,
		},
		{
			name: "malformed data URL",
			parts: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,%%%"}},
			want: ErrBadImage,
		},
		{
			name: "oversized",
			parts: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,YWJjZA=="}},
			opts: TransformOptions{MaxImageBytes: 3},
			want: ErrImageTooLarge,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			in := protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: tc.parts}}}}
			out, err := TransformRequest(context.Background(), in, testVisionCandidate(), transformDescriberFunc(func(context.Context, sidecar.Candidate, Request) (Result, error) {
				calls++
				return Result{Description: "unused"}, nil
			}), tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if calls != 0 {
				t.Fatalf("describer called %d time(s) before validation failed", calls)
			}
			if !reflect.DeepEqual(out, protocol.ParsedRequest{}) {
				t.Fatalf("out=%#v", out)
			}
		})
	}
}

func TestTransformRequestHonorsTimeoutAndDescriptionBound(t *testing.T) {
	in := protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{
		Role: protocol.RoleUser,
		Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,YQ=="}},
	}}}}

	t.Run("timeout", func(t *testing.T) {
		out, err := TransformRequest(context.Background(), in, testVisionCandidate(), transformDescriberFunc(func(ctx context.Context, _ sidecar.Candidate, _ Request) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		}), TransformOptions{Timeout: time.Millisecond})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
		if !reflect.DeepEqual(out, protocol.ParsedRequest{}) {
			t.Fatalf("out=%#v", out)
		}
	})

	t.Run("description bound", func(t *testing.T) {
		out, err := TransformRequest(context.Background(), in, testVisionCandidate(), transformDescriberFunc(func(context.Context, sidecar.Candidate, Request) (Result, error) {
			return Result{Description: "123456789"}, nil
		}), TransformOptions{MaxDescription: 5})
		if err != nil {
			t.Fatal(err)
		}
		got := out.Context.Messages[0].Content[0].Text
		if !strings.Contains(got, "\n12345\n") || strings.Contains(got, "123456") {
			t.Fatalf("bounded description=%q", got)
		}
	})
}
