package kiro

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const (
	maxKiroImages      = 20
	maxKiroImageBudget = 18 << 20
)

type Image struct {
	Format string
	Bytes  string
}

func ExtractImages(parts []protocol.ContentPart) ([]Image, error) {
	var out []Image
	var budget int
	for _, part := range parts {
		if part.Type != protocol.ContentImage {
			continue
		}
		img, err := parseKiroDataURL(part.ImageURL)
		if err != nil {
			return nil, err
		}
		if img == nil {
			continue
		}
		if len(out) >= maxKiroImages {
			return nil, fmt.Errorf("Kiro image count exceeds per-message cap")
		}
		budget += len(img.Bytes)
		if budget > maxKiroImageBudget {
			return nil, fmt.Errorf("Kiro image payload exceeds byte budget")
		}
		out = append(out, *img)
	}
	return out, nil
}

func parseKiroDataURL(raw string) (*Image, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, "data:") {
		return nil, fmt.Errorf("Kiro images accept only data: parts")
	}
	meta, rest, ok := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
	if !ok || rest == "" {
		return nil, fmt.Errorf("Kiro image data URL is incomplete")
	}
	if _, err := base64.StdEncoding.Strict().DecodeString(rest); err != nil {
		return nil, fmt.Errorf("Kiro image payload is not strict base64")
	}
	mime, _, _ := strings.Cut(meta, ";")
	subtype := mime
	if _, after, ok := strings.Cut(mime, "/"); ok {
		subtype = after
	}
	format := strings.ToLower(subtype)
	if format == "jpg" {
		format = "jpeg"
	}
	return &Image{Format: format, Bytes: rest}, nil
}
