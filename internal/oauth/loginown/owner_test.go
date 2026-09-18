package loginown

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/authpublic"
)

func TestSupersededOwnerCannotPersist(t *testing.T) {
	ResetForTest()
	first, err := TryClaim("xai")
	if err != nil {
		t.Fatal(err)
	}
	if !Cancel("xai") {
		t.Fatal("expected cancel")
	}
	second, err := TryClaim("xai")
	if err != nil {
		t.Fatal(err)
	}
	if err := Assert(first); !errors.As(err, &authpublic.LoginSupersededError{}) {
		t.Fatalf("first persist=%v", err)
	}
	if err := Assert(second); err != nil {
		t.Fatalf("second persist=%v", err)
	}
	Release(second)
}

func TestTryClaimRejectsInProgressDuplicate(t *testing.T) {
	ResetForTest()
	if _, err := TryClaim("xai"); err != nil {
		t.Fatal(err)
	}
	_, err := TryClaim("xai")
	var busy authpublic.LoginBusyError
	if !errors.As(err, &busy) || busy.Error() != "A login for xai is already in progress" {
		t.Fatalf("busy=%v", err)
	}
}

func TestReplacementWaitsForExternalHelperSettle(t *testing.T) {
	ResetForTest()
	BeginSettle("kiro")
	_, err := TryClaim("kiro")
	if !errors.As(err, &authpublic.LoginBusyError{}) {
		t.Fatalf("settling claim=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		EndSettle("kiro")
	}()
	if err := WaitNotSettling(ctx, "kiro"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if _, err := TryClaim("kiro"); err != nil {
		t.Fatal(err)
	}
}
