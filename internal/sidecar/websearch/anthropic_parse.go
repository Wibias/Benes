package websearch

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/authpublic"
)

func parseAnthropicSidecarSSE(r io.Reader) (Result, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var frame strings.Builder
	out := Result{}
	seen := map[string]struct{}{}
	var streamErr error
	flush := func() {
		raw := frame.String()
		frame.Reset()
		if strings.TrimSpace(raw) == "" {
			return
		}
		var dataLine strings.Builder
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimPrefix(line, "data:")
				payload = strings.TrimPrefix(payload, " ")
				dataLine.WriteString(payload)
			}
		}
		payload := dataLine.String()
		if payload == "" || payload == "[DONE]" {
			return
		}
		var event map[string]any
		if json.Unmarshal([]byte(payload), &event) != nil {
			return
		}
		switch event["type"] {
		case "error":
			streamErr = authpublic.SidecarStream()
			return
		case "content_block_start":
			block, _ := event["content_block"].(map[string]any)
			if block["type"] != "web_search_tool_result" {
				return
			}
			switch content := block["content"].(type) {
			case []any:
				for _, item := range content {
					rec, _ := item.(map[string]any)
					if rec["type"] != "web_search_result" {
						continue
					}
					pushAnthropicSource(&out, seen, rec["url"], rec["title"])
				}
			}
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			switch delta["type"] {
			case "text_delta":
				if text, ok := delta["text"].(string); ok {
					out.Text += text
				}
			case "citations_delta":
				citation, _ := delta["citation"].(map[string]any)
				if citation["type"] == "web_search_result_location" {
					pushAnthropicSource(&out, seen, citation["url"], citation["title"])
				}
			}
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if frame.Len() > 0 {
			frame.WriteByte('\n')
		}
		frame.WriteString(line)
	}
	flush()
	if err := scanner.Err(); err != nil {
		return Result{}, authpublic.SidecarStream()
	}
	if streamErr != nil {
		return Result{}, streamErr
	}
	out.Sources = SanitizeSources(out.Sources)
	return out, nil
}

func pushAnthropicSource(out *Result, seen map[string]struct{}, urlVal, titleVal any) {
	url, _ := urlVal.(string)
	if url == "" {
		return
	}
	if _, ok := seen[url]; ok {
		return
	}
	seen[url] = struct{}{}
	title, _ := titleVal.(string)
	out.Sources = append(out.Sources, Source{URL: url, Title: title})
}
