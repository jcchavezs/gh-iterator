package iterator

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/require"
)

func overrideExecerFactory(t *testing.T, execFactory func(string, *slog.Logger) exec.Execer) {
	t.Helper()
	oldFactory := newExecerWithLogger
	newExecerWithLogger = execFactory
	t.Cleanup(func() {
		newExecerWithLogger = oldFactory
	})
}

func TestCheckCloneability(t *testing.T) {
	ctx := context.Background()

	t.Run("empty repo pages returns error", func(t *testing.T) {
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{}
		})

		err := checkCloneability(ctx, [][]Repository{}, func(Repository) bool { return true }, false)
		require.Error(t, err)
		require.EqualError(t, err, "no repositories to check cloneability")
	})

	t.Run("successful cloneability check with SSH", func(t *testing.T) {
		repoPages := [][]Repository{
			{
				{
					Name:   "test-org/test-repo",
					SSHURL: "git@github.com:test-org/test-repo.git",
					URL:    "https://github.com/test-org/test-repo.git",
				},
			},
		}

		var capturedCommand string
		var capturedArgs []string

		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					capturedCommand = command
					capturedArgs = args
					return "", nil
				},
			}
		})

		err := checkCloneability(ctx, repoPages, func(Repository) bool { return true }, false)
		require.NoError(t, err)

		require.Equal(t, "git", capturedCommand)

		expectedArgs := []string{"ls-remote", "--exit-code", "git@github.com:test-org/test-repo.git"}
		require.Equal(t, expectedArgs, capturedArgs)
	})

	t.Run("failed cloneability check", func(t *testing.T) {
		repoPages := [][]Repository{
			{
				{
					Name:   "test-org/test-repo",
					SSHURL: "git@github.com:test-org/test-repo.git",
					URL:    "https://github.com/test-org/test-repo.git",
				},
			},
		}

		mockErr := errors.New("authentication failed")
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					return "", mockErr
				},
			}
		})

		err := checkCloneability(ctx, repoPages, func(Repository) bool { return true }, false)
		require.Error(t, err)
		require.ErrorIs(t, err, mockErr)
	})

	t.Run("all cloneability checks fail returns all errors", func(t *testing.T) {
		repoPages := [][]Repository{
			{
				{
					Name:   "test-org/repo-1",
					SSHURL: "git@github.com:test-org/repo-1.git",
					URL:    "https://github.com/test-org/repo-1.git",
				},
				{
					Name:   "test-org/repo-2",
					SSHURL: "git@github.com:test-org/repo-2.git",
					URL:    "https://github.com/test-org/repo-2.git",
				},
			},
		}

		mockErr1 := errors.New("authentication failed for repo-1")
		mockErr2 := errors.New("authentication failed for repo-2")
		callCount := 0
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					callCount++
					if callCount == 1 {
						return "", mockErr1
					}
					return "", mockErr2
				},
			}
		})

		err := checkCloneability(ctx, repoPages, func(Repository) bool { return true }, false)
		require.Error(t, err)
		require.ErrorIs(t, err, mockErr1)
		require.ErrorIs(t, err, mockErr2)
		require.Equal(t, 2, callCount)
	})

	t.Run("first cloneability check fails but next succeed returns no error", func(t *testing.T) {
		repoPages := [][]Repository{
			{
				{
					Name:   "test-org/repo-1",
					SSHURL: "git@github.com:test-org/repo-1.git",
					URL:    "https://github.com/test-org/repo-1.git",
				},
				{
					Name:   "test-org/repo-2",
					SSHURL: "git@github.com:test-org/repo-2.git",
					URL:    "https://github.com/test-org/repo-2.git",
				},
			},
		}

		callCount := 0
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					callCount++
					if callCount == 1 {
						return "", errors.New("authentication failed")
					}
					return "", nil
				},
			}
		})

		err := checkCloneability(ctx, repoPages, func(Repository) bool { return true }, false)
		require.NoError(t, err)
		require.Equal(t, 2, callCount)
	})

	t.Run("filters out repositories correctly", func(t *testing.T) {
		repoPages := [][]Repository{
			{
				{
					Name: "test-org/archived-repo",
				},
				{
					Name:   "test-org/active-repo",
					SSHURL: "git@github.com:test-org/active-repo.git",
					URL:    "https://github.com/test-org/active-repo.git",
				},
			},
		}

		var capturedArgs []string

		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					capturedArgs = args
					return "", nil
				},
			}
		})

		// Filter that only accepts "active-repo"
		filterIn := func(r Repository) bool {
			return r.Name == "test-org/active-repo"
		}

		err := checkCloneability(ctx, repoPages, filterIn, false)
		require.NoError(t, err)

		// Should use the active-repo URL, not the archived one
		expectedURL := "git@github.com:test-org/active-repo.git"
		require.GreaterOrEqual(t, len(capturedArgs), 3)
		require.Equal(t, expectedURL, capturedArgs[2])
	})
}
