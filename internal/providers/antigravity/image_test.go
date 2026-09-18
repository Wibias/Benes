package antigravity

import (
	"errors"
	"testing"
)

func TestAmbiguousPaidImageFailureIsNonRetryable(t *testing.T) {
	if err := ImageTransportFailure(false); err != nil {
		t.Fatalf("pre-dispatch=%v", err)
	}
	err := ImageTransportFailure(true)
	if !errors.Is(err, ErrAmbiguousPaidImage) {
		t.Fatalf("post-dispatch=%v", err)
	}
}
