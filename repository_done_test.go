package iterator

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/require"
)

func TestOnRepositoryDone(t *testing.T) {
	noopProcessor := func(context.Context, Repository, exec.Execer) error { return nil }

	t.Run("without repository context is a no-op", func(t *testing.T) {
		OnRepositoryDone(context.Background(), func(error) { t.Fatal("must not be called") })
	})

	t.Run("runs after success in reverse order, once per repository", func(t *testing.T) {
		var calls []string
		opts := Options{ContextEnricher: func(ctx context.Context, r Repository) context.Context {
			OnRepositoryDone(ctx, func(err error) { calls = append(calls, r.Name+":first:"+errStr(err)) })
			OnRepositoryDone(ctx, func(err error) { calls = append(calls, r.Name+":second:"+errStr(err)) })
			return ctx
		}}

		for _, name := range []string{"a", "b"} {
			require.NoError(t, processRepository(context.Background(), Repository{Name: name}, noopProcessor, opts))
		}
		require.Equal(t, []string{"a:second:", "a:first:", "b:second:", "b:first:"}, calls)
	})

	t.Run("receives the processor error", func(t *testing.T) {
		boom := errors.New("boom")
		var got error
		opts := Options{ContextEnricher: func(ctx context.Context, _ Repository) context.Context {
			OnRepositoryDone(ctx, func(err error) { got = err })
			return ctx
		}}

		err := processRepository(context.Background(), Repository{Name: "a"},
			func(context.Context, Repository, exec.Execer) error { return boom }, opts)
		require.ErrorIs(t, err, boom)
		require.ErrorIs(t, got, boom)
	})

	t.Run("runs when cloning fails", func(t *testing.T) {
		cloneErr := errors.New("clone failed")
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{RunXFn: func(context.Context, string, ...string) (string, error) { return "", cloneErr }}
		})

		var got error
		var processed bool
		opts := Options{ContextEnricher: func(ctx context.Context, _ Repository) context.Context {
			OnRepositoryDone(ctx, func(err error) { got = err })
			return ctx
		}}

		err := processRepository(context.Background(), Repository{Name: "o/a", Size: 10, DefaultBranchName: "main"},
			func(context.Context, Repository, exec.Execer) error { processed = true; return nil }, opts)
		require.Error(t, err)
		require.False(t, processed)
		require.ErrorIs(t, got, cloneErr)
	})
}

func TestWrapExecer(t *testing.T) {
	t.Run("wraps the processor execer, including derived ones", func(t *testing.T) {
		var wrapped atomic.Int32
		opts := Options{WrapExecer: func(x exec.Execer) exec.Execer {
			wrapped.Add(1)
			return x
		}}

		err := processRepository(context.Background(), Repository{Name: "a"},
			func(_ context.Context, _ Repository, x exec.Execer) error {
				require.NotNil(t, x)
				return nil
			}, opts)
		require.NoError(t, err)
		require.EqualValues(t, 1, wrapped.Load())
	})

	t.Run("wraps the internal execers used to clone", func(t *testing.T) {
		var factoryCalls int
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			factoryCalls++
			return mock.Execer{RunXFn: func(context.Context, string, ...string) (string, error) { return "", nil }}
		})

		var wrapped int
		opts := Options{WrapExecer: func(x exec.Execer) exec.Execer {
			wrapped++
			return x
		}}

		_ = processRepository(context.Background(), Repository{Name: "o/a", Size: 10, DefaultBranchName: "main"},
			func(context.Context, Repository, exec.Execer) error { return nil }, opts)
		require.Positive(t, factoryCalls)
		// every internal execer plus the processor's one goes through the wrapper
		require.GreaterOrEqual(t, wrapped, factoryCalls)
	})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type ctxKey struct{}

func TestOnRepository(t *testing.T) {
	t.Run("ctx reaches the processor and done receives the error, after the enricher's", func(t *testing.T) {
		boom := errors.New("boom")
		var calls []string
		opts := Options{
			ContextEnricher: func(ctx context.Context, _ Repository) context.Context {
				calls = append(calls, "enricher")
				OnRepositoryDone(ctx, func(error) { calls = append(calls, "enricher-done") })
				return ctx
			},
			OnRepository: func(ctx context.Context, r Repository) (context.Context, func(error)) {
				calls = append(calls, "on-repository")
				return context.WithValue(ctx, ctxKey{}, r.Name), func(err error) {
					calls = append(calls, "on-repository-done:"+errStr(err))
				}
			},
		}

		err := processRepository(context.Background(), Repository{Name: "a"},
			func(ctx context.Context, _ Repository, _ exec.Execer) error {
				require.Equal(t, "a", ctx.Value(ctxKey{}))
				return boom
			}, opts)
		require.ErrorIs(t, err, boom)
		require.Equal(t, []string{"enricher", "on-repository", "on-repository-done:processing empty repository: boom", "enricher-done"}, calls)
	})

	t.Run("nil ctx and nil done are tolerated", func(t *testing.T) {
		opts := Options{OnRepository: func(context.Context, Repository) (context.Context, func(error)) { return nil, nil }}
		require.NoError(t, processRepository(context.Background(), Repository{Name: "a"},
			func(ctx context.Context, _ Repository, _ exec.Execer) error {
				require.NotNil(t, ctx)
				return nil
			}, opts))
	})

	t.Run("done runs when cloning fails", func(t *testing.T) {
		cloneErr := errors.New("clone failed")
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{RunXFn: func(context.Context, string, ...string) (string, error) { return "", cloneErr }}
		})

		var got error
		opts := Options{OnRepository: func(ctx context.Context, _ Repository) (context.Context, func(error)) {
			return ctx, func(err error) { got = err }
		}}

		err := processRepository(context.Background(), Repository{Name: "o/a", Size: 10, DefaultBranchName: "main"},
			func(context.Context, Repository, exec.Execer) error { return nil }, opts)
		require.Error(t, err)
		require.ErrorIs(t, got, cloneErr)
	})
}

func TestViewRepositoryOptions(t *testing.T) {
	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		x := mock.Execer{}
		x.RunXFn = func(context.Context, string, ...string) (string, error) {
			return `{"full_name":"o/a"}`, nil
		}
		x.WithEnvFn = func(...string) exec.Execer { return x }
		return x
	})

	var wrapped, started int
	var doneErr error
	boom := errors.New("boom")

	err := ViewRepository(context.Background(), "o/a",
		func(context.Context, Repository, exec.Execer) error { return boom },
		ViewRepositoriesOptions{
			WrapExecer: func(x exec.Execer) exec.Execer { wrapped++; return x },
			OnRepository: func(ctx context.Context, _ Repository) (context.Context, func(error)) {
				started++
				return ctx, func(err error) { doneErr = err }
			},
		})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, wrapped)
	require.Equal(t, 1, started)
	require.ErrorIs(t, doneErr, boom)
}
