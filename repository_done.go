package iterator

import (
	"context"
	"sync"
)

type doneHooksKey struct{}

type doneHooks struct {
	mu  sync.Mutex
	fns []func(err error)
}

func (h *doneHooks) add(fn func(err error)) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.fns = append(h.fns, fn)
}

// run calls the registered functions in reverse order of registration.
func (h *doneHooks) run(err error) {
	h.mu.Lock()
	fns := h.fns
	h.fns = nil
	h.mu.Unlock()

	for i := len(fns) - 1; i >= 0; i-- {
		fns[i](err)
	}
}

// beginRepository runs the per-repository hooks (ContextEnricher and OnRepository) and
// returns the context to use for the repository and a function to call, with the final
// error, once the repository is done.
func (o Options) beginRepository(ctx context.Context, repo Repository) (context.Context, func(err error)) {
	hooks := &doneHooks{}
	ctx = context.WithValue(ctx, doneHooksKey{}, hooks)

	if o.ContextEnricher != nil {
		ctx = o.ContextEnricher(ctx, repo)
	}

	if o.OnRepository != nil {
		hookCtx, done := o.OnRepository(ctx, repo)
		if hookCtx != nil {
			ctx = hookCtx
		}
		if done != nil {
			hooks.add(done)
		}
	}

	return ctx, hooks.run
}

// OnRepositoryDone registers fn to be called synchronously once the processing of the
// repository, including cloning, has finished. err is the error the processing
// ended with, nil on success. Functions run in reverse order of registration.
//
// It can be called from Options.ContextEnricher or the processor with the context they
// receive. Prefer returning a done function from Options.OnRepository. It does nothing if
// ctx does not belong to a repository processing.
func OnRepositoryDone(ctx context.Context, fn func(err error)) {
	if h, ok := ctx.Value(doneHooksKey{}).(*doneHooks); ok {
		h.add(fn)
	}
}
