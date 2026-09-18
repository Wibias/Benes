package timeline

import (
	"context"
	"testing"
)

func TestWithTraceRoundTripsFromContext(t *testing.T) {
	tr := New("req-ctx", 8)
	ctx := WithTrace(context.Background(), tr)
	if got := FromContext(ctx); got != tr {
		t.Fatalf("got=%p want=%p", got, tr)
	}
}

func TestFromContextIsNilWithoutTrace(t *testing.T) {
	if FromContext(context.Background()) != nil {
		t.Fatal("empty context must not invent a trace")
	}
	if FromContext(nil) != nil {
		t.Fatal("nil context must not invent a trace")
	}
	if FromContext(WithTrace(context.Background(), nil)) != nil {
		t.Fatal("nil trace must not be stored")
	}
}
