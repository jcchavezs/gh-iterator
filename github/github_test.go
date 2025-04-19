package github

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/stretchr/testify/require"
)

func requireNoErrorAndPrintStderr(t *testing.T, err error) {
	t.Helper()
	if stderr, ok := exec.GetStderr(err); ok {
		_, _ = os.Stderr.WriteString(stderr)
	}

	require.NoError(t, err)
}

func createRepo(t *testing.T) exec.Execer {
	ctx := context.Background()
	dir := t.TempDir()
	x := exec.NewExecer(dir, false)
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
