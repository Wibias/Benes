package modelprobe

import (
	"context"
	"testing"
)

func TestFinishTransportErrorClassifiesClientDeadlineBeforeContext(t *testing.T) {
	got, err := finishTransportError(context.Background(), Result{}, context.DeadlineExceeded)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateUnavailable || got.Reason != "timeout" {
		t.Fatalf("deadline error=%#v", got)
	}
}
