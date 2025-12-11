package github

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/require"
)

func requireNoErrorAndPrintStderr(t *testing.T, err error) {
	t.Helper()
	if stderr, ok := iteratorexec.GetStderr(err); ok {
		_, _ = os.Stderr.WriteString(stderr)
	}

	require.NoError(t, err)
}

func createRepo(t *testing.T) iteratorexec.Execer {
	ctx := context.Background()
	dir := t.TempDir()
	x := iteratorexec.NewExecer(dir)
	_, err := x.RunX(ctx, "git", "init")
	requireNoErrorAndPrintStderr(t, err)

	_, err = x.RunX(ctx, "git", "config", "--local", "commit.gpgsign", "false")
	requireNoErrorAndPrintStderr(t, err)

	_, err = x.RunX(ctx, "git", "config", "--local", "user.email", "test@github.com")
	requireNoErrorAndPrintStderr(t, err)

	_, err = x.RunX(ctx, "git", "config", "--local", "user.name", "test github")
	requireNoErrorAndPrintStderr(t, err)

	_, err = x.RunWithStdinX(ctx, strings.NewReader("Hello world!"), "tee", "README.md")
	requireNoErrorAndPrintStderr(t, err)

	_, err = x.RunX(ctx, "git", "add", "README.md")
	requireNoErrorAndPrintStderr(t, err)

	_, err = x.RunX(ctx, "git", "commit", "-m", "docs: adds readme")
	requireNoErrorAndPrintStderr(t, err)

	return x
}

func TestCheckoutNewBranch(t *testing.T) {
	exec := createRepo(t)
	err := CheckoutNewBranch(context.Background(), exec, "new_branch")
	requireNoErrorAndPrintStderr(t, err)

	b, err := CurrentBranch(context.Background(), exec)
	requireNoErrorAndPrintStderr(t, err)
	require.Equal(t, "new_branch", b)
}

func TestAddFiles(t *testing.T) {
	exec := createRepo(t)

	ctx := context.Background()
	_, err := exec.RunX(ctx, "touch", "NEW_FILE.md")
	requireNoErrorAndPrintStderr(t, err)

	err = AddFiles(ctx, exec, "NEW_FILE.md")
	requireNoErrorAndPrintStderr(t, err)

	status, err := exec.RunX(ctx, "git", "status", "-s")
	requireNoErrorAndPrintStderr(t, err)
	require.Contains(t, status, "NEW_FILE.md")

	_, err = exec.RunX(ctx, "touch", "NEW_FILE.txt")
	requireNoErrorAndPrintStderr(t, err)

	err = AddFiles(ctx, exec, "**/*.txt")
	requireNoErrorAndPrintStderr(t, err)

	status, err = exec.RunX(ctx, "git", "status", "-s")
	requireNoErrorAndPrintStderr(t, err)
	require.Contains(t, status, "NEW_FILE.txt")
}

func TestHasChanges(t *testing.T) {
	exec := createRepo(t)

	ctx := context.Background()
	_, err := exec.RunWithStdinX(ctx, strings.NewReader("Modified content"), "tee", "README.md")
	requireNoErrorAndPrintStderr(t, err)

	hasChanges, err := HasChanges(ctx, exec)
	requireNoErrorAndPrintStderr(t, err)
	require.True(t, hasChanges)
}

func TestListChanges(t *testing.T) {
	exec := createRepo(t)

	ctx := context.Background()
	_, err := exec.RunWithStdinX(ctx, strings.NewReader("Modified content"), "tee", "README.md")
	requireNoErrorAndPrintStderr(t, err)

	changes, err := ListChanges(ctx, exec)
	requireNoErrorAndPrintStderr(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "M", changes[0][0])
	require.Equal(t, "README.md", changes[0][1])
}

func TestCommit(t *testing.T) {
	exec := createRepo(t)

	ctx := context.Background()
	_, err := exec.RunWithStdinX(ctx, strings.NewReader("Modified content"), "tee", "README.md")
	requireNoErrorAndPrintStderr(t, err)

	err = AddFiles(ctx, exec, "README.md")
	requireNoErrorAndPrintStderr(t, err)

	err = Commit(ctx, exec, "feat: update readme")
	requireNoErrorAndPrintStderr(t, err)

	log, err := exec.RunX(ctx, "git", "log", "--oneline", "-1")
	requireNoErrorAndPrintStderr(t, err)
	require.Contains(t, log, "feat: update readme")
}

func TestCreatePRIfNotExist(t *testing.T) {
	t.Run("error viewing PR", func(t *testing.T) {
		x := mock.Execer{
			RunFn: func(ctx context.Context, command string, args ...string) (iteratorexec.Result, error) {
				return iteratorexec.Result{}, errors.New("failed to view PR")
			},
		}
		_, _, err := CreatePRIfNotExist(context.Background(), x, PROptions{})
		require.Error(t, err)
	})

	t.Run("existing PR", func(t *testing.T) {
		x := mock.Execer{
			RunFn: func(ctx context.Context, command string, args ...string) (iteratorexec.Result, error) {
				return iteratorexec.Result{
					Stdout: `{
						"url": "https://github.com/jcchavezs/gh-iterator/pull/21",
						"state": "OPEN",
						"isDraft": false
					}`,
				}, nil
			},
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return "", nil
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		prURL, isNewPR, err := CreatePRIfNotExist(context.Background(), x, PROptions{})
		require.NoError(t, err)
		require.Equal(t, false, isNewPR)
		require.Equal(t, "https://github.com/jcchavezs/gh-iterator/pull/21", prURL)
	})

	t.Run("new PR", func(t *testing.T) {
		x := mock.Execer{
			RunFn: func(ctx context.Context, command string, args ...string) (iteratorexec.Result, error) {
				return iteratorexec.Result{
					ExitCode: 1,
				}, nil
			},
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				t.Logf("RunX command: %s, args: %v", command, args)
				require.Len(t, args, 3)
				require.Equal(t, "gh", command)
				require.Equal(t, "pr", args[0])
				require.Equal(t, "create", args[1])
				require.Equal(t, "--fill", args[2])
				return "https://github.com/jcchavezs/gh-iterator/pull/22", nil
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		prURL, isNewPR, err := CreatePRIfNotExist(context.Background(), x, PROptions{})
		require.NoError(t, err)
		require.Equal(t, true, isNewPR)
		require.Equal(t, "https://github.com/jcchavezs/gh-iterator/pull/22", prURL)
	})
}

func TestForkAndAddRemote(t *testing.T) {
	t.Run("successful fork and add remote", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				if command == "gh" && len(args) >= 1 && args[0] == "api" {
					// Mock the getCurrentUser call
					require.Len(t, args, 4)
					require.Equal(t, "user", args[1])
					require.Equal(t, "--jq", args[2])
					require.Equal(t, ".login", args[3])
					return "testuser", nil
				}

				if command == "gh" && len(args) >= 1 && args[0] == "repo" {
					// Mock the fork call
					require.Len(t, args, 5)
					require.Equal(t, "fork", args[1])
					require.Equal(t, "--remote", args[2])
					require.Equal(t, "--remote-name", args[3])
					require.Equal(t, "upstream", args[4])
					return "", nil
				}
				return "", errors.New("unexpected command")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		headRefFn, err := ForkAndAddRemote(context.Background(), x, "upstream")
		require.NoError(t, err)
		require.NotNil(t, headRefFn)

		// Test the returned function
		headRef := headRefFn("feature-branch")
		require.Equal(t, "testuser:feature-branch", headRef)

		headRef = headRefFn("main")
		require.Equal(t, "testuser:main", headRef)
	})

	t.Run("error getting current user", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				if command == "gh" && len(args) >= 1 && args[0] == "api" {
					return "", errors.New("failed to get user")
				}
				return "", errors.New("unexpected command")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		headRefFn, err := ForkAndAddRemote(context.Background(), x, "fork")
		require.Error(t, err)
		require.Nil(t, headRefFn)
		require.Contains(t, err.Error(), "getting current user")
	})

	t.Run("error forking repository", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				if command == "gh" && len(args) >= 1 && args[0] == "api" {
					// Mock the getCurrentUser call
					return "testuser", nil
				}
				if command == "gh" && len(args) >= 1 && args[0] == "repo" {
					// Mock the fork call
					return "", errors.New("failed to fork repository")
				}
				return "", errors.New("unexpected command")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		headRefFn, err := ForkAndAddRemote(context.Background(), x, "fork")
		require.Error(t, err)
		require.Nil(t, headRefFn)
		require.Contains(t, err.Error(), "forking repository and adding remote")
	})
}
