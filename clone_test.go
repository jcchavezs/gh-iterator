package iterator

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestCloneDirNameNoCollisions(t *testing.T) {
	repo := Repository{Name: "org/repo"}
	subset := []string{"a"}
	hash := hashCloningSubset(subset)

	// cache keys that mimic the suffixes added to the directory name
	require.NotEqual(t,
		cloneDirName(repo, "key-depth5", RunOptions{}),
		cloneDirName(repo, "key", RunOptions{FetchDepth: 5}),
	)
	require.NotEqual(t,
		cloneDirName(repo, "key-subset"+hash, RunOptions{}),
		cloneDirName(repo, "key", RunOptions{CloningSubset: subset}),
	)
	require.NotEqual(t,
		cloneDirName(repo, "key-"+hash, RunOptions{}),
		cloneDirName(repo, "key", RunOptions{CloningSubset: subset}),
	)
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

// runCloneAndRecordGit clones with the given options and returns the git calls made
// after `git init`, each joined by spaces.
func runCloneAndRecordGit(t *testing.T, repo Repository, opts RunOptions) ([]string, error) {
	t.Helper()

	var calls []string
	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
				if command == "git" && len(args) > 0 && (args[0] == "fetch" || args[0] == "checkout") {
					calls = append(calls, command+" "+strings.Join(args, " "))
				}

				return "", nil
			},
		}
	})

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git/info"), os.ModePerm)) // git init is mocked

	return calls, cloneRepository(context.Background(), repo, dir, opts)
}

func TestCloneRepositoryRef(t *testing.T) {
	const sha = "2fc5ceb5183ee739da15ac7d30c39717303182c4"
	repo := Repository{SSHURL: "u", DefaultBranchName: "main"}

	t.Run("without ref the default branch is fetched and checked out", func(t *testing.T) {
		calls, err := runCloneAndRecordGit(t, repo, RunOptions{})
		require.NoError(t, err)
		require.Equal(t, []string{
			"git fetch --no-tags --depth=1 origin main",
			"git checkout main",
		}, calls)
	})

	for name, ref := range map[string]string{"sha": sha, "tag": "v1.2.3", "branch": "release/1.x"} {
		t.Run("ref as "+name, func(t *testing.T) {
			calls, err := runCloneAndRecordGit(t, repo, RunOptions{Ref: ref})
			require.NoError(t, err)
			require.Equal(t, []string{
				"git fetch --no-tags --depth=1 origin " + ref,
				"git checkout FETCH_HEAD",
			}, calls)
		})
	}

	t.Run("ref with subset and depth", func(t *testing.T) {
		calls, err := runCloneAndRecordGit(t, repo, RunOptions{Ref: sha, FetchDepth: 3, CloningSubset: []string{"go.mod"}})
		require.NoError(t, err)
		require.Equal(t, []string{
			"git fetch --no-tags --filter=blob:none --depth=3 origin " + sha,
			"git checkout FETCH_HEAD",
		}, calls)
	})

	t.Run("ref does not need a default branch", func(t *testing.T) {
		calls, err := runCloneAndRecordGit(t, Repository{SSHURL: "u"}, RunOptions{Ref: sha})
		require.NoError(t, err)
		require.Len(t, calls, 2)
	})

	t.Run("no ref and no default branch fails", func(t *testing.T) {
		_, err := runCloneAndRecordGit(t, Repository{SSHURL: "u"}, RunOptions{})
		require.ErrorIs(t, err, errNoDefaultBranch)
	})

	t.Run("ref looking like an option is rejected", func(t *testing.T) {
		calls, err := runCloneAndRecordGit(t, repo, RunOptions{Ref: "--upload-pack=evil"})
		require.Error(t, err)
		require.Empty(t, calls, "nothing must be fetched")
	})
}

func TestCloneDirNameRef(t *testing.T) {
	repo := Repository{Name: "org/repo"}
	name := func(o RunOptions) string { return cloneDirName(repo, "key", o) }

	require.NotEqual(t, name(RunOptions{}), name(RunOptions{Ref: "main"}))
	require.NotEqual(t, name(RunOptions{Ref: "v1"}), name(RunOptions{Ref: "v2"}))
	require.Equal(t, name(RunOptions{Ref: "v1"}), name(RunOptions{Ref: "v1"}))
	require.NotEqual(t,
		cloneDirName(repo, "key-ref"+hashRef("v1"), RunOptions{}),
		cloneDirName(repo, "key", RunOptions{Ref: "v1"}),
	)
}

func TestRunForOrganizationRejectsRef(t *testing.T) {
	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
				t.Errorf("unexpected command %s %v", command, args)
				return "", nil
			},
		}
	})

	_, err := RunForOrganization(context.Background(), "org", SearchOptions{}, nil, RunOptions{Ref: "main"})
	require.ErrorIs(t, err, errRefNotSupportedForOrganization)
}

func TestCloneRepositoryOrGetFromCacheConcurrent(t *testing.T) {
	const workers = 5

	repo := Repository{Name: "org/concurrent-" + randSequence(6), SSHURL: "u", DefaultBranchName: "main"}
	opts := RunOptions{CloneCacheKey: CloneCacheKeyFromString("key")}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(reposDir, cloneDirName(repo, "key", opts))) })

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

func TestCloneRepositoryOrGetFromCacheCancelledWaiter(t *testing.T) {
	repo := Repository{Name: "org/cancel-" + randSequence(6), SSHURL: "u", DefaultBranchName: "main"}
	opts := RunOptions{CloneCacheKey: CloneCacheKeyFromString("key")}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(reposDir, cloneDirName(repo, "key", opts))) })

	cloning, release := make(chan struct{}), make(chan struct{})
	var once sync.Once

	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
				if mock.CallIs(t, command, args, "git", "init") {
					once.Do(func() { close(cloning) })
					<-release // slow clone
				}

				return "", nil
			},
		}
	})

	firstDone := make(chan error, 1)
	go func() {
		_, err := cloneRepositoryOrGetFromCache(context.Background(), repo, opts)
		firstDone <- err
	}()
	<-cloning

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := cloneRepositoryOrGetFromCache(ctx, repo, opts)
		secondDone <- err
	}()

	cancel()
	select {
	case err := <-secondDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled waiter is still blocked by the active clone")
	}

	close(release)
	require.NoError(t, <-firstDone)
}
