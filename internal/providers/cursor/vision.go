package cursor

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

var visionModels = map[string]struct{}{
	"gpt-5.3-codex":     {},
	"gpt-5.4":           {},
	"claude-4.5-sonnet": {},
	"claude-4.6-sonnet": {},
}

const (
	maxVisionBytes     = 8 << 20
	maxVisionSoftBytes = 2 << 20
	maxVisionCount     = 8
	maxVisionEdge      = 2000
	maxVisionHighEdge  = 4096
)

func PromoteActiveImages(parsed protocol.ParsedRequest) ([]string, error) {
	model := strings.TrimSpace(parsed.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(parsed.ModelID)
	}
	if _, ok := visionModels[model]; !ok {
		return nil, nil
	}
	var out []string
	active := activeMessageIndex(parsed.Context.Messages)
	for i, message := range parsed.Context.Messages {
		if i != active {
			continue
		}
		if message.Role != protocol.RoleUser && message.Role != protocol.RoleDeveloper {
			continue
		}
		for _, part := range message.Content {
			if part.Type != protocol.ContentImage {
				continue
			}
			payload, err := strictDataURL(part.ImageURL)
			if err != nil {
				return nil, err
			}
			payload, err = boundVisionPayload(payload, part.Detail)
			if err != nil {
				return nil, err
			}
			out = append(out, payload)
			if len(out) > maxVisionCount {
				return nil, fmt.Errorf("Cursor vision exceeds per-turn image count")
			}
		}
	}
	return out, nil
}

func strictDataURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "data:") {
		return "", fmt.Errorf("Cursor vision accepts only data: image parts")
	}
	_, rest, ok := strings.Cut(raw, ",")
	if !ok || rest == "" {
		return "", fmt.Errorf("Cursor vision data URL is incomplete")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(rest)
	if err != nil {
		return "", fmt.Errorf("Cursor vision payload is not strict base64")
	}
	if len(decoded) == 0 || len(decoded) > maxVisionBytes {
		return "", fmt.Errorf("Cursor vision payload exceeds decode-byte ceiling")
	}
	if !knownImagePrefix(decoded) {
		return "", fmt.Errorf("Cursor vision payload is not a recognized image")
	}
	return raw, nil
}

func boundVisionPayload(dataURL, detail string) (string, error) {
	mime, data, err := splitDataURL(dataURL)
	if err != nil {
		return "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if len(data) > maxVisionSoftBytes && !highVisionDetail(detail) {
			return "", fmt.Errorf("Cursor vision payload exceeds soft budget")
		}
		return dataURL, nil
	}
	edge := maxVisionEdge
	if highVisionDetail(detail) {
		edge = maxVisionHighEdge
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return "", fmt.Errorf("Cursor vision dimensions are invalid")
	}
	if int64(cfg.Width)*int64(cfg.Height) > int64(edge)*int64(edge) {
		return "", fmt.Errorf("Cursor vision exceeds pixel ceiling")
	}
	if cfg.Width <= edge && cfg.Height <= edge && len(data) <= maxVisionSoftBytes {
		return dataURL, nil
	}
	if cfg.Width <= edge && cfg.Height <= edge && highVisionDetail(detail) && len(data) <= maxVisionBytes {
		return dataURL, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("Cursor vision payload is undecodable")
	}
	shrunk := shrinkImage(img, edge)
	var buf bytes.Buffer
	if err := png.Encode(&buf, shrunk); err != nil {
		return "", fmt.Errorf("Cursor vision re-encode failed")
	}
	if buf.Len() > maxVisionBytes {
		return "", fmt.Errorf("Cursor vision payload exceeds decode-byte ceiling")
	}
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func highVisionDetail(detail string) bool {
	switch strings.ToLower(strings.TrimSpace(detail)) {
	case "high", "original":
		return true
	default:
		return false
	}
}

func shrinkImage(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return src
	}
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func knownImagePrefix(raw []byte) bool {
	switch {
	case len(raw) >= 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff:
		return true
	case len(raw) >= 6 && (string(raw[:6]) == "GIF87a" || string(raw[:6]) == "GIF89a"):
		return true
	case len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		return true
	default:
		return false
	}
}
