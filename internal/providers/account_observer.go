package providers

import "context"

// CommittedUsageAccount is the safe, non-PII Usage identity for a committed
// credential. Callers must not pass emails, tokens, ChatGPT account ids, or
// raw managed-account UUIDs.
type CommittedUsageAccount string

type CommittedUsageAccountObserver interface {
	NoteCommittedUsageAccount(account CommittedUsageAccount)
}

type committedUsageAccountObserverKey struct{}

func WithCommittedUsageAccountObserver(ctx context.Context, observer CommittedUsageAccountObserver) context.Context {
	if ctx == nil || observer == nil {
		return ctx
	}
	return context.WithValue(ctx, committedUsageAccountObserverKey{}, observer)
}

func CommittedUsageAccountObserverFrom(ctx context.Context) CommittedUsageAccountObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(committedUsageAccountObserverKey{}).(CommittedUsageAccountObserver)
	return observer
}
