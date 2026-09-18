package main

import (
	"context"
	"time"
)

const logFollowInterval = time.Second

func commandSleep(deps commandDependencies, d time.Duration) {
	if deps.sleep != nil {
		deps.sleep(d)
		return
	}
	time.Sleep(d)
}

// followWait sleeps one follow interval. It returns false when ctx is already
// cancelled or is cancelled during the wait so `benes logs -f` can exit on SIGINT.
func followWait(ctx context.Context, deps commandDependencies) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	commandSleep(deps, logFollowInterval)
	return ctx == nil || ctx.Err() == nil
}
