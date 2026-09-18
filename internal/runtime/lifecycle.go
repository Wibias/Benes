package runtime

import (
	"context"
	"sync"
)

type Group struct {
	ctx    context.Context
	cancel context.CancelCauseFunc

	wg   sync.WaitGroup
	once sync.Once
	err  error
}

func NewGroup(parent context.Context) *Group {
	ctx, cancel := context.WithCancelCause(parent)
	return &Group{ctx: ctx, cancel: cancel}
}

func (g *Group) Go(fn func(context.Context) error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := fn(g.ctx); err != nil {
			g.once.Do(func() {
				g.err = err
				g.cancel(err)
			})
		}
	}()
}

func (g *Group) Wait() error {
	g.wg.Wait()
	if g.err != nil {
		return g.err
	}
	if err := g.ctx.Err(); err != nil {
		return err
	}
	return nil
}
