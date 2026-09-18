package combo

import (
	"context"
	"errors"
	"testing"

	"github.com/Wibias/Benes/internal/timeline"
)

func TestAttemptsForIsGoogleScoped(t *testing.T) {
	if AttemptsFor("google") != 3 || AttemptsFor("google-vertex") != 3 {
		t.Fatal("google members keep bounded transient retry")
	}
	if AttemptsFor("openai-chat") != 1 || AttemptsFor("cursor") != 1 || AttemptsFor("kiro") != 1 {
		t.Fatal("non-google combo targets must remain reset-only")
	}
}

func TestWalkRetriesGoogleThenHopsToNextMember(t *testing.T) {
	var saw []string
	err := Walk(context.Background(), []Member{
		{Protocol: "google", ID: "g"},
		{Protocol: "openai-chat", ID: "o"},
	}, func(_ context.Context, member Member, attempts int) (Result, error) {
		saw = append(saw, member.ID)
		if member.Protocol == "google" {
			if attempts != 3 {
				t.Fatalf("google attempts=%d", attempts)
			}
			return Result{Status: 503, Message: "unavailable"}, nil
		}
		if attempts != 1 {
			t.Fatalf("openai attempts=%d", attempts)
		}
		return Result{Status: 200}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saw) != 2 || saw[0] != "g" || saw[1] != "o" {
		t.Fatalf("saw=%v", saw)
	}
}

func TestWalkStampsPerMemberAttemptOnTheRequestTrace(t *testing.T) {
	tr := timeline.New("req-combo", 8)
	ctx := timeline.WithTrace(context.Background(), tr)
	var seen []int
	err := Walk(ctx, []Member{
		{Protocol: "google", ID: "g"},
		{Protocol: "openai-chat", ID: "o"},
	}, func(ctx context.Context, member Member, _ int) (Result, error) {
		seen = append(seen, timeline.FromContext(ctx).Attempt())
		if member.Protocol == "google" {
			return Result{Status: 503, Message: "unavailable"}, nil
		}
		return Result{Status: 200}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("seen=%v events=%#v", seen, tr.Events())
	}
}

func TestWalkDoesNotRetryNonGoogleBeforeHop(t *testing.T) {
	calls := 0
	err := Walk(context.Background(), []Member{
		{Protocol: "cursor", ID: "c"},
		{Protocol: "google", ID: "g"},
	}, func(_ context.Context, member Member, attempts int) (Result, error) {
		calls++
		if member.Protocol == "cursor" {
			if attempts != 1 {
				t.Fatalf("cursor attempts=%d", attempts)
			}
			return Result{Status: 503, Message: "unavailable"}, nil
		}
		return Result{Status: 200}, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestWalkStopsOnNonHopFailure(t *testing.T) {
	calls := 0
	err := Walk(context.Background(), []Member{
		{Protocol: "google", ID: "g"},
		{Protocol: "openai-chat", ID: "o"},
	}, func(_ context.Context, member Member, attempts int) (Result, error) {
		calls++
		return Result{Status: 400, Message: "invalid_request_error"}, nil
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestWalkHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Walk(ctx, []Member{{Protocol: "google", ID: "g"}}, func(context.Context, Member, int) (Result, error) {
		t.Fatal("must not attempt after cancel")
		return Result{}, errors.New("unreachable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestFailureDecisionHopsTransientAndStopsInvalid(t *testing.T) {
	if FailureDecision(503, "unavailable", "") != DecisionHop {
		t.Fatal("503")
	}
	if FailureDecision(429, "rate", "") != DecisionHop {
		t.Fatal("429")
	}
	if FailureDecision(400, "invalid_request_error", "invalid_request_error") != DecisionStop {
		t.Fatal("400")
	}
	if FailureDecision(499, "canceled", "") != DecisionStop {
		t.Fatal("499")
	}
	if FailureDecision(410, "gone", "") != DecisionHop {
		t.Fatal("410")
	}
}
