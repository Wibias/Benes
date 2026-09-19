package transport

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

func DoPhysicalSend(ctx context.Context, client *http.Client, req *http.Request, turn *resourcebudget.Turn, reason string) (*http.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	if req == nil {
		return nil, fmt.Errorf("HTTP request is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var lease *resourcebudget.PhysicalSendLease
	var err error
	if turn != nil {
		lease, err = turn.ReservePhysicalSend(reason)
		if err != nil {
			return nil, err
		}
		defer lease.Release()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lease != nil {
		if err := lease.Commit(); err != nil {
			return nil, err
		}
	}
	return client.Do(req)
}
