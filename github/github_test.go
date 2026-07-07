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
	t.Run("adding single file", func(t *testing.T) {
		exec := createRepo(t)

		ctx := context.Background()
		_, err := exec.RunX(ctx, "touch", "NEW_FILE.md")
		requireNoErrorAndPrintStderr(t, err)

		err = AddFiles(ctx, exec, "NEW_FILE.md")
		requireNoErrorAndPrintStderr(t, err)

		status, err := exec.RunX(ctx, "git", "status", "-s")
		requireNoErrorAndPrintStderr(t, err)
		require.Contains(t, status, "NEW_FILE.md")
	})

	t.Run("adding single file", func(t *testing.T) {
		exec := createRepo(t)
		ctx := context.Background()

		_, err := exec.RunX(ctx, "touch", "NEW_FILE.txt")
		requireNoErrorAndPrintStderr(t, err)

		_, err = exec.RunX(ctx, "mkdir", "-p", "nested")
		requireNoErrorAndPrintStderr(t, err)

		_, err = exec.RunX(ctx, "touch", "nested/NEW_FILE2.txt")
		requireNoErrorAndPrintStderr(t, err)

		err = AddFiles(ctx, exec, "**/*.txt")
		requireNoErrorAndPrintStderr(t, err)

		status, err := exec.RunX(ctx, "git", "status", "-s")
		requireNoErrorAndPrintStderr(t, err)
		require.Contains(t, status, "NEW_FILE.txt")
		require.Contains(t, status, "NEW_FILE2.txt")
	})
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
				if mock.CallIs(t, command, args, "gh", "pr", "create", "--fill") {
					return "https://github.com/jcchavezs/gh-iterator/pull/22", nil
				}

				return "", mock.ErrUnexpectedCall
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		prURL, isNewPR, err := CreatePRIfNotExist(context.Background(), x, PROptions{})
		require.NoError(t, err)
		require.Equal(t, true, isNewPR)
		require.Equal(t, "https://github.com/jcchavezs/gh-iterator/pull/22", prURL)
	})

	t.Run("assignees", func(t *testing.T) {
		t.Run("on new PR", func(t *testing.T) {
			xr := mock.Execer{
				RunFn: func(ctx context.Context, command string, args ...string) (iteratorexec.Result, error) {
					return iteratorexec.Result{
						ExitCode: 1,
					}, nil
				},
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					t.Logf("RunX command: %s, args: %v", command, args)
					if mock.CallIs(t, command, args, "gh", "pr", "create", "--body-file", mock.CallAny, "--title", mock.CallAny, "--assignee", "testuser") {
						return "", nil
					}

					return "", mock.ErrUnexpectedCall
				},
				Logger: slog.New(slog.DiscardHandler),
			}

			_, _, err := CreatePRIfNotExist(context.Background(), xr, PROptions{
				Title:     "Test PR",
				Body:      "This is a test PR",
				Assignees: []string{"testuser"},
			})
			require.NoError(t, err)
		})

		t.Run("on existing PR", func(t *testing.T) {
			xr := mock.Execer{
				RunFn: func(ctx context.Context, command string, args ...string) (iteratorexec.Result, error) {
					return iteratorexec.Result{
						Stdout: `{
							"url": "https://github.com/jcchavezs/gh-iterator/pull/21",
							"state": "OPEN",
							"isDraft": false,
							"assignees": [
								{"id": "U_kgDOCbOMzw", "login": "testuser"}
							]
						}`,
					}, nil
				},
				RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
					t.Logf("RunX command: %s, args: %v", command, args)
					if mock.CallIs(t, command, args, "gh", "pr", "edit", mock.CallAny, "--body-file", mock.CallAny, "--title", "Test PR", "--add-assignee", "testuser2") {
						return "", nil
					}

					return "", mock.ErrUnexpectedCall
				},
				Logger: slog.New(slog.DiscardHandler),
			}

			prURL, isNewPR, err := CreatePRIfNotExist(context.Background(), xr, PROptions{
				Title:     "Test PR",
				Body:      "This is a test PR",
				Assignees: []string{"testuser2"},
			})
			require.NoError(t, err)
			require.Equal(t, false, isNewPR)
			require.Equal(t, "https://github.com/jcchavezs/gh-iterator/pull/21", prURL)
		})
	})
}

func TestIsRepositoryArchived(t *testing.T) {
	t.Run("archived repository", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return "true\n", nil
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		archived, err := IsRepositoryArchived(context.Background(), "owner/repo", x)
		require.NoError(t, err)
		require.True(t, archived)
	})

	t.Run("non-archived repository", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return "false\n", nil
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		archived, err := IsRepositoryArchived(context.Background(), "owner/repo", x)
		require.NoError(t, err)
		require.False(t, archived)
	})

	t.Run("error checking archived status", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return "", errors.New("API error")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		archived, err := IsRepositoryArchived(context.Background(), "owner/repo", x)
		require.Error(t, err)
		require.False(t, archived)
		require.Contains(t, err.Error(), "checking if repository is archived")
	})
}

func TestReadFile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				require.Equal(t, "gh", command)
				require.Equal(t, []string{"api", "/repos/owner/repo/contents/path/to/file.txt", "-H", "Accept: application/vnd.github.raw+json"}, args)
				return "file contents", nil
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		content, err := ReadFile(context.Background(), x, "owner/repo", "path/to/file.txt")
		require.NoError(t, err)
		require.Equal(t, []byte("file contents"), content)
	})

	t.Run("file not found", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return `{"message":"Not Found","status":"404"}`, errors.New("exit status 1")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		content, err := ReadFile(context.Background(), x, "owner/repo", "missing.txt")
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Nil(t, content)
	})

	t.Run("api error with message", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return `{"message":"Bad credentials","status":"401"}`, errors.New("exit status 1")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		content, err := ReadFile(context.Background(), x, "owner/repo", "file.txt")
		require.Error(t, err)
		require.Contains(t, err.Error(), "Bad credentials")
		require.Nil(t, content)
	})

	t.Run("unexpected error", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				return "", errors.New("network failure")
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		content, err := ReadFile(context.Background(), x, "owner/repo", "file.txt")
		require.Error(t, err)
		require.Contains(t, err.Error(), "network failure")
		require.Nil(t, content)
	})
}

func TestForkAndAddRemote(t *testing.T) {
	t.Run("successful fork and add remote", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				if mock.CallIs(t, command, args, "gh", "api", "user", "--jq", ".login") {
					return "testuser", nil
				}
				if mock.CallIs(t, command, args, "gh", "repo", "fork", "--remote", "--remote-name", "upstream") {
					return "", nil
				}
				return "", mock.ErrUnexpectedCall
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		headRefFn, err := ForkAndAddRemote(context.Background(), x, "upstream")
		require.NoError(t, err)
		require.NotNil(t, headRefFn)

		headRef := headRefFn("feature-branch")
		require.Equal(t, "testuser:feature-branch", headRef)

		headRef = headRefFn("main")
		require.Equal(t, "testuser:main", headRef)
	})

	t.Run("error getting current user", func(t *testing.T) {
		x := mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				if mock.CallIs(t, command, args, "gh", "api") {
					return "", errors.New("failed to get user")
				}
				return "", mock.ErrUnexpectedCall
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
				if mock.CallIs(t, command, args, "gh", "api") {
					return "testuser", nil
				}
				if mock.CallIs(t, command, args, "gh", "repo", "fork") {
					return "", errors.New("failed to fork repository")
				}
				return "", mock.ErrUnexpectedCall
			},
			Logger: slog.New(slog.DiscardHandler),
		}

		headRefFn, err := ForkAndAddRemote(context.Background(), x, "fork")
		require.Error(t, err)
		require.Nil(t, headRefFn)
		require.Contains(t, err.Error(), "forking repository and adding remote")
	})
}
