package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupCancelsSiblingsAfterFirstFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	group := NewGroup(ctx)
	var siblingCancelled atomic.Bool

	group.Go(func(context.Context) error {
		return errors.New("boom")
	})
	group.Go(func(ctx context.Context) error {
		<-ctx.Done()
		siblingCancelled.Store(true)
		return nil
	})

	err := group.Wait()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Wait() error = %v, want boom", err)
	}
	if !siblingCancelled.Load() {
		t.Fatal("sibling did not observe cancellation")
	}
}

func TestGroupReturnsContextErrorWhenParentExpires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	group := NewGroup(ctx)

	group.Go(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	cancel()

	if err := group.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
}
