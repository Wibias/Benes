package openairesponses

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestClassifyForward403DenialRootStringCodeMasksNestedCode(t *testing.T) {
	for _, body := range []string{
		"{\"code\":\"\",\"error\":{\"code\":\"workspace_access_denied\"}}",
		"{\"code\":\"other\",\"error\":{\"code\":\"workspace_access_denied\"}}",
	} {
		if got := classifyForward403Denial(context.Background(), io.NopCloser(strings.NewReader(body))); got != "" {
			t.Fatalf("body=%s denial=%q", body, got)
		}
	}
}
