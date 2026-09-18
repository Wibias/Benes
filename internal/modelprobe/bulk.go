package modelprobe

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

const (
	DefaultConcurrency = 3
	MaxConcurrency     = 8
)

func ClampConcurrency(n int) int {
	if n <= 0 {
		return DefaultConcurrency
	}
	if n > MaxConcurrency {
		return MaxConcurrency
	}
	return n
}

func ProbeAll(ctx context.Context, base Request, models []string, concurrency int) []Result {
	if ctx == nil {
		ctx = context.Background()
	}
	out := make([]Result, len(models))
	workers := ClampConcurrency(concurrency)
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, model := range models {
		i, model := i, model
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			req := base
			req.Model = model
			result, err := Probe(ctx, req)
			if err != nil {
				out[i] = UnknownResult(req.Provider, model)
				out[i].CredentialSlot = req.CredentialSlot
				if parsed, parseErr := url.Parse(strings.TrimSpace(req.BaseURL)); parseErr == nil {
					out[i].DestinationHost = strings.ToLower(parsed.Hostname())
				}
				if ctx.Err() != nil {
					out[i].Reason = "canceled"
					return
				}
				out[i].Reason = "probe_failed"
				return
			}
			out[i] = result
		}()
	}
	wg.Wait()
	return out
}
