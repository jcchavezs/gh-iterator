package iterator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/require"
)

func TestViewRepositoriesInOrganization(t *testing.T) {
	ctx := context.Background()

	t.Run("processes every repository returned by the API", func(t *testing.T) {
		const pages = `[{"full_name":"org/repo-1","size":1,"default_branch":"main"},{"full_name":"org/repo-2","size":1,"default_branch":"main"}]
`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			var m mock.Execer
			m = mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					if mock.CallIs(t, command, args, "gh", "api") {
						return pages, nil
					}
					return "", mock.ErrUnexpectedCall
				},
				WithEnvFn: func(kv ...string) exec.Execer { return m },
			}
			return m
		})

		var (
			mu     sync.Mutex
			viewed []string
		)
		callback := func(ctx context.Context, xr exec.Execer, repo Repository) error {
			mu.Lock()
			defer mu.Unlock()
			viewed = append(viewed, repo.Name)
			return nil
		}

		result, err := ViewRepositoriesInOrganization(ctx, "org", SearchOptions{}, callback, ViewRepositoriesOptions{})
		require.NoError(t, err)
		require.Equal(t, 2, result.Found)
		require.Equal(t, 2, result.Inspected)
		require.Equal(t, 2, result.Processed)

		mu.Lock()
		defer mu.Unlock()
		require.ElementsMatch(t, []string{"org/repo-1", "org/repo-2"}, viewed)
	})

	t.Run("honours the search filter", func(t *testing.T) {
		const pages = `[{"full_name":"org/go-repo","size":1,"language":"Go","default_branch":"main"},{"full_name":"org/py-repo","size":1,"language":"Python","default_branch":"main"}]
`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			var m mock.Execer
			m = mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return pages, nil
				},
				WithEnvFn: func(kv ...string) exec.Execer { return m },
			}
			return m
		})

		var (
			mu     sync.Mutex
			viewed []string
		)
		callback := func(ctx context.Context, xr exec.Execer, repo Repository) error {
			mu.Lock()
			defer mu.Unlock()
			viewed = append(viewed, repo.Name)
			return nil
		}

		result, err := ViewRepositoriesInOrganization(ctx, "org", SearchOptions{Languages: []string{"Go"}}, callback, ViewRepositoriesOptions{})
		require.NoError(t, err)
		require.Equal(t, 2, result.Found)
		require.Equal(t, 2, result.Inspected)
		require.Equal(t, 1, result.Processed)

		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, []string{"org/go-repo"}, viewed)
	})

	t.Run("returns error when fetching repositories fails", func(t *testing.T) {
		fetchErr := errors.New("boom")
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return "", fetchErr
				},
			}
		})

		callback := func(ctx context.Context, xr exec.Execer, repo Repository) error {
			t.Fatal("callback should not be invoked")
			return nil
		}

		_, err := ViewRepositoriesInOrganization(ctx, "org", SearchOptions{}, callback, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, fetchErr)
	})

	t.Run("propagates callback error", func(t *testing.T) {
		const pages = `[{"full_name":"org/repo-1","size":1,"default_branch":"main"}]
`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			var m mock.Execer
			m = mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return pages, nil
				},
				WithEnvFn: func(kv ...string) exec.Execer { return m },
			}
			return m
		})

		callbackErr := errors.New("callback failed")
		callback := func(ctx context.Context, xr exec.Execer, repo Repository) error {
			return callbackErr
		}

		_, err := ViewRepositoriesInOrganization(ctx, "org", SearchOptions{}, callback, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, callbackErr)
	})
}

func TestViewRepository(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects an invalid repository name", func(t *testing.T) {
		err := ViewRepository(ctx, "too/many/slashes", func(context.Context, exec.Execer, Repository) error {
			t.Fatal("callback should not be invoked")
			return nil
		}, ViewRepositoriesOptions{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "incorrect repository name")
	})

	t.Run("views an empty repository", func(t *testing.T) {
		const repo = `{"full_name":"org/empty-repo","size":0}`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					if mock.CallIs(t, command, args, "gh", "api") {
						return repo, nil
					}
					return "", mock.ErrUnexpectedCall
				},
			}
		})

		var viewed Repository
		err := ViewRepository(ctx, "org/empty-repo", func(ctx context.Context, xr exec.Execer, r Repository) error {
			viewed = r
			return nil
		}, ViewRepositoriesOptions{})
		require.NoError(t, err)
		require.Equal(t, "org/empty-repo", viewed.Name)
		require.True(t, viewed.IsEmpty())
	})

	t.Run("returns error when fetching the repository fails", func(t *testing.T) {
		fetchErr := errors.New("boom")
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return "", fetchErr
				},
			}
		})

		err := ViewRepository(ctx, "org/repo", func(context.Context, exec.Execer, Repository) error {
			t.Fatal("callback should not be invoked")
			return nil
		}, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, fetchErr)
	})

	t.Run("returns error when the repository payload is invalid", func(t *testing.T) {
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return "not-json", nil
				},
			}
		})

		err := ViewRepository(ctx, "org/repo", func(context.Context, exec.Execer, Repository) error {
			t.Fatal("callback should not be invoked")
			return nil
		}, ViewRepositoriesOptions{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unmarshaling repository")
	})

	t.Run("propagates callback error", func(t *testing.T) {
		const repo = `{"full_name":"org/empty-repo","size":0}`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return repo, nil
				},
			}
		})

		callbackErr := errors.New("callback failed")
		err := ViewRepository(ctx, "org/empty-repo", func(context.Context, exec.Execer, Repository) error {
			return callbackErr
		}, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, callbackErr)
	})
}
