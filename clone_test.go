package iterator

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchArgs(t *testing.T) {
	tests := map[string]struct {
		depth    int
		blobless bool
		expected []string
	}{
		"default is shallow": {0, false, []string{"fetch", "--no-tags", "--depth=1", "origin", "main"}},
		"explicit depth":     {5, false, []string{"fetch", "--no-tags", "--depth=5", "origin", "main"}},
		"negative is full":   {-1, false, []string{"fetch", "--no-tags", "origin", "main"}},
		"blobless":           {0, true, []string{"fetch", "--no-tags", "--filter=blob:none", "--depth=1", "origin", "main"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, fetchArgs(tc.depth, "main", tc.blobless))
		})
	}
}

func TestCloneDirName(t *testing.T) {
	repo := Repository{Name: "org/repo"}
	name := func(o RunOptions) string { return cloneDirName(repo, "key", o) }

	require.Equal(t, name(RunOptions{}), name(RunOptions{FetchDepth: 1}), "default equals depth 1")
	require.NotEqual(t, name(RunOptions{}), name(RunOptions{FetchDepth: 5}))
	require.NotEqual(t, name(RunOptions{FetchDepth: 5}), name(RunOptions{FetchDepth: 10}))
	require.NotEqual(t, name(RunOptions{}), name(RunOptions{FetchDepth: -1}))
	require.NotEqual(t,
		name(RunOptions{CloningSubset: []string{"a"}, FetchDepth: 5}),
		name(RunOptions{CloningSubset: []string{"a"}}),
	)
}

func TestCopyArgs(t *testing.T) {
	require.Equal(t, []string{"-cR", "a", "b"}, copyArgs("darwin", "a", "b"))
	require.Equal(t, []string{"-r", "--reflink=auto", "a", "b"}, copyArgs("linux", "a", "b"))
	require.Equal(t, []string{"-r", "a", "b"}, copyArgs("windows", "a", "b"))
}

// runCloneAndExpectFetch clones with the given options and reports whether
// a git fetch call matching the expected arguments was made.
func runCloneAndExpectFetch(t *testing.T, opts RunOptions, expectedFetch ...any) bool {
	t.Helper()

	var fetched bool
	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
				if mock.CallIs(t, command, args, expectedFetch...) {
					fetched = true
				}

				return "", nil
			},
		}
	})

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git/info"), os.ModePerm)) // git init is mocked

	repo := Repository{SSHURL: "u", DefaultBranchName: "main"}
	require.NoError(t, cloneRepository(context.Background(), repo, dir, opts))

	return fetched
}

func TestCloneRepositoryFetchDepth(t *testing.T) {
	require.True(t, runCloneAndExpectFetch(t, RunOptions{},
		"git", "fetch", "--no-tags", "--depth=1", "origin", "main"))
	require.True(t, runCloneAndExpectFetch(t, RunOptions{FetchDepth: 3},
		"git", "fetch", "--no-tags", "--depth=3", "origin", "main"))
	require.True(t, runCloneAndExpectFetch(t, RunOptions{FetchDepth: -1},
		"git", "fetch", "--no-tags", "origin", "main"))
}

func TestCloneRepositoryBloblessOnlyWithSubset(t *testing.T) {
	t.Run("without subset", func(t *testing.T) {
		require.True(t, runCloneAndExpectFetch(t, RunOptions{},
			"git", "fetch", "--no-tags", "--depth=1", "origin", "main"))
	})

	t.Run("with subset", func(t *testing.T) {
		require.True(t, runCloneAndExpectFetch(t, RunOptions{CloningSubset: []string{"README.md"}},
			"git", "fetch", "--no-tags", "--filter=blob:none", "--depth=1", "origin", "main"))
	})
}

func TestCloneRepositoryOrGetFromCacheConcurrent(t *testing.T) {
	const workers = 5

	repo := Repository{Name: "org/concurrent-" + randSequence(6), SSHURL: "u", DefaultBranchName: "main"}
	opts := RunOptions{CloneCacheKey: CloneCacheKeyFromString("key")}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(reposDir, repo.Name+"-key")) })

	var (
		inits        atomic.Int32
		cloneDone    atomic.Bool
		copiedEarly  atomic.Int32
		copiesCalled atomic.Int32
	)

	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
				switch {
				case mock.CallIs(t, command, args, "git", "init"):
					inits.Add(1)
					time.Sleep(100 * time.Millisecond) // widen the race window
				case mock.CallIs(t, command, args, "git", "checkout"):
					cloneDone.Store(true)
				case command == "cp":
					copiesCalled.Add(1)
					if !cloneDone.Load() {
						copiedEarly.Add(1)
					}
				}

				return "", nil
			},
		}
	})

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, err := cloneRepositoryOrGetFromCache(context.Background(), repo, opts)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	require.EqualValues(t, 1, inits.Load(), "repository must be cloned once")
	require.EqualValues(t, workers, copiesCalled.Load())
	require.Zero(t, copiedEarly.Load(), "cache must not be copied while it is still being cloned")
}
