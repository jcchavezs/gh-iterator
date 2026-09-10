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
		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
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
		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
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

		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
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
		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
			return callbackErr
		}

		_, err := ViewRepositoriesInOrganization(ctx, "org", SearchOptions{}, callback, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, callbackErr)
	})
}

func TestViewRepository(t *testing.T) {
	ctx := context.Background()

	t.Run("fetches the repository and invokes the processor", func(t *testing.T) {
		const repo = `{"full_name":"org/repo","clone_url":"https://github.com/org/repo.git","default_branch":"main","language":"Go","size":42}`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			var m mock.Execer
			m = mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					if mock.CallIs(t, command, args, "gh", "api") {
						return repo, nil
					}
					return "", mock.ErrUnexpectedCall
				},
				WithEnvFn: func(kv ...string) exec.Execer { return m },
			}
			return m
		})

		var processed Repository
		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
			processed = repo
			return nil
		}

		err := ViewRepository(ctx, "org/repo", callback, ViewRepositoriesOptions{})
		require.NoError(t, err)
		require.Equal(t, "org/repo", processed.Name)
		require.Equal(t, "main", processed.DefaultBranchName)
		require.Equal(t, "Go", processed.Language)
		require.Equal(t, 42, processed.Size)
	})

	t.Run("rejects an incorrect repository name", func(t *testing.T) {
		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
			t.Fatal("callback should not be invoked")
			return nil
		}

		err := ViewRepository(ctx, "org/group/repo", callback, ViewRepositoriesOptions{})
		require.ErrorContains(t, err, `incorrect repository name "org/group/repo"`)
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

		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
			t.Fatal("callback should not be invoked")
			return nil
		}

		err := ViewRepository(ctx, "org/repo", callback, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, fetchErr)
	})

	t.Run("returns error when the response is not valid JSON", func(t *testing.T) {
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return "not-json", nil
				},
			}
		})

		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
			t.Fatal("callback should not be invoked")
			return nil
		}

		err := ViewRepository(ctx, "org/repo", callback, ViewRepositoriesOptions{})
		require.ErrorContains(t, err, "unmarshaling repository")
	})

	t.Run("propagates processor error", func(t *testing.T) {
		const repo = `{"full_name":"org/repo","default_branch":"main"}`
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			var m mock.Execer
			m = mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return repo, nil
				},
				WithEnvFn: func(kv ...string) exec.Execer { return m },
			}
			return m
		})

		processorErr := errors.New("processor failed")
		callback := func(ctx context.Context, repo Repository, xr exec.Execer) error {
			return processorErr
		}

		err := ViewRepository(ctx, "org/repo", callback, ViewRepositoriesOptions{})
		require.ErrorIs(t, err, processorErr)
	})
}
