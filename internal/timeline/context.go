package timeline

import "context"

type traceContextKey struct{}

func WithTrace(ctx context.Context, tr *Trace) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tr == nil {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey{}, tr)
}

func FromContext(ctx context.Context) *Trace {
	if ctx == nil {
		return nil
	}
	tr, _ := ctx.Value(traceContextKey{}).(*Trace)
	return tr
}
