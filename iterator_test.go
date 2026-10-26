package iterator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestFillLines(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test")
	require.NoError(t, err)

	err = fillLines(f.Name(), []string{"README.md", "LICENSE"})
	require.NoError(t, err)

	require.NoError(t, f.Close())

	f, err = os.Open(f.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, f.Close())
	})

	fc, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "README.md\nLICENSE\n", string(fc))
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func mockLSRemoteCheck(t *testing.T) {
	t.Helper()
	overrideExecerFactory(t, func(s string, l *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(ctx context.Context, command string, args ...string) (string, error) {
				if command == "git" && len(args) >= 3 && args[0] == "ls-remote" {
					return "", nil
				}
				return "", errors.New("unexpected command")
			},
		}
	})
}

func TestRunForReposConcurrently(t *testing.T) {
	ctx := context.Background()

	// Generate a large number of repositories for testing
	var repoPages [][]Repository
	for i := range 10 {
		var page []Repository
		for j := range 100 {
			page = append(page, Repository{Name: fmt.Sprintf("repo-%d-%d", i, j)})
		}
		repoPages = append(repoPages, page)
	}

	nOfWorkers := 5
	var processedRepos []string
	var processedMux sync.Mutex

	processor := func(ctx context.Context, repository Repository, xr exec.Execer) error {
		processedMux.Lock()
		defer processedMux.Unlock()
		processedRepos = append(processedRepos, repository.Name)
		return nil
	}

	opts := Options{
		Debug: true,
	}

	mockLSRemoteCheck(t)

	result, err := runForReposConcurrently(ctx, pagesChan(repoPages), nOfWorkers, func(repo Repository) bool { return true }, processRepository, processor, opts)
	require.NoError(t, err)
	require.Equal(t, 1000, result.Found)
	require.Equal(t, 1000, result.Inspected)
	require.Equal(t, 1000, result.Processed)

	processedMux.Lock()
	defer processedMux.Unlock()
	require.Len(t, processedRepos, 1000)
}

func TestRunForReposConcurrentlyFilteredRepos(t *testing.T) {
	ctx := context.Background()

	repoPages := [][]Repository{
		{
			{Name: "repo1", Language: "Go"},
			{Name: "repo2", Language: "Python"},
		},
		{
			{Name: "repo3", Language: "Go"},
		},
	}

	nOfWorkers := 2
	var processedRepos []string
	var processedMux sync.Mutex

	processor := func(ctx context.Context, repository Repository, xr exec.Execer) error {
		processedMux.Lock()
		defer processedMux.Unlock()
		processedRepos = append(processedRepos, repository.Name)
		return nil
	}

	opts := Options{
		Debug: true,
	}

	filterIn := func(repo Repository) bool {
		return repo.Language == "Go"
	}

	mockLSRemoteCheck(t)

	result, err := runForReposConcurrently(ctx, pagesChan(repoPages), nOfWorkers, filterIn, processRepository, processor, opts)
	require.NoError(t, err)
	require.Equal(t, 3, result.Found)
	require.Equal(t, 3, result.Inspected)
	require.Equal(t, 2, result.Processed)

	processedMux.Lock()
	defer processedMux.Unlock()
	require.ElementsMatch(t, []string{"repo1", "repo3"}, processedRepos)
}

func TestRunForReposConcurrentlyEmptyRepos(t *testing.T) {
	ctx := context.Background()

	repoPages := [][]Repository{}

	nOfWorkers := 2
	processor := func(ctx context.Context, repository Repository, xr exec.Execer) error {
		return nil
	}

	opts := Options{}

	result, err := runForReposConcurrently(ctx, pagesChan(repoPages), nOfWorkers, func(repo Repository) bool { return true }, processRepository, processor, opts)
	require.NoError(t, err)
	require.Equal(t, 0, result.Found)
	require.Equal(t, 0, result.Inspected)
	require.Equal(t, 0, result.Processed)
}

func TestRunForReposConcurrentlyContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	repoPages := [][]Repository{
		{
			{Name: "repo1"},
			{Name: "repo2"},
		},
		{
			{Name: "repo3"},
		},
	}

	nOfWorkers := 2
	processor := func(ctx context.Context, repository Repository, xr exec.Execer) error {
		cancel() // Cancel the context during processing
		return nil
	}

	opts := Options{}
	mockLSRemoteCheck(t)

	_, err := runForReposConcurrently(ctx, pagesChan(repoPages), nOfWorkers, func(repo Repository) bool { return true }, processRepository, processor, opts)

	require.ErrorIs(t, err, context.Canceled)
}

func TestRunForReposConcurrentlyErrorInProcessor(t *testing.T) {
	ctx := context.Background()

	repoPages := [][]Repository{
		{
			{Name: "repo1"},
			{Name: "repo2"},
		},
	}

	nOfWorkers := 2
	processor := func(ctx context.Context, repository Repository, xr exec.Execer) error {
		if repository.Name == "repo2" {
			return fmt.Errorf("error processing %s", repository.Name)
		}
		return nil
	}

	opts := Options{}

	mockLSRemoteCheck(t)

	_, err := runForReposConcurrently(ctx, pagesChan(repoPages), nOfWorkers, func(repo Repository) bool { return true }, processRepository, processor, opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "error processing repo2")
}

// pagesChan returns a closed stream with the given pages.
func pagesChan(repoPages [][]Repository) <-chan repoPage {
	c := make(chan repoPage, len(repoPages))
	for _, repos := range repoPages {
		c <- repoPage{repos: repos}
	}
	close(c)
	return c
}

func collectPages(t *testing.T, pages <-chan repoPage) ([][]Repository, error) {
	t.Helper()
	var out [][]Repository
	for p := range pages {
		if p.err != nil {
			return out, p.err
		}
		out = append(out, p.repos)
	}
	return out, nil
}

func repoPageJSON(n int) string {
	repos := make([]string, n)
	for i := range repos {
		repos[i] = fmt.Sprintf(`{"full_name":"org/repo-%d"}`, i)
	}
	return "[" + strings.Join(repos, ",") + "]"
}

func TestStreamRepoPages(t *testing.T) {
	// recordTargets mocks gh returning fullPages full pages followed by a short one.
	recordTargets := func(t *testing.T, perPage, fullPages int) *[]string {
		t.Helper()
		var targets []string
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(_ context.Context, _ string, args ...string) (string, error) {
					target := args[len(args)-1]
					targets = append(targets, target)
					if len(targets) <= fullPages {
						return repoPageJSON(perPage), nil
					}
					return repoPageJSON(1), nil
				},
			}
		})
		return &targets
	}

	t.Run("all pages stops at the first short page", func(t *testing.T) {
		targets := recordTargets(t, 2, 2)
		pages, err := streamRepoPages(context.Background(), SearchOptions{Page: AllPages, PerPage: 2}, "org", slog.Default())
		require.NoError(t, err)

		got, err := collectPages(t, pages)
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, []string{
			"/orgs/org/repos?per_page=2&page=1",
			"/orgs/org/repos?per_page=2&page=2",
			"/orgs/org/repos?per_page=2&page=3",
		}, *targets)
	})

	t.Run("a single page is fetched by default", func(t *testing.T) {
		targets := recordTargets(t, 100, 5)
		pages, err := streamRepoPages(context.Background(), SearchOptions{}, "org", slog.Default())
		require.NoError(t, err)

		got, err := collectPages(t, pages)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, []string{"/orgs/org/repos?per_page=100&page=1"}, *targets)
	})

	t.Run("a specific page", func(t *testing.T) {
		targets := recordTargets(t, 100, 5)
		pages, err := streamRepoPages(context.Background(), SearchOptions{Page: PageN(3)}, "org", slog.Default())
		require.NoError(t, err)

		_, err = collectPages(t, pages)
		require.NoError(t, err)
		require.Equal(t, []string{"/orgs/org/repos?per_page=100&page=3"}, *targets)
	})

	t.Run("pushes the type down", func(t *testing.T) {
		targets := recordTargets(t, 100, 0)
		pages, err := streamRepoPages(context.Background(), SearchOptions{Source: OnlyNonForks, Visibility: VisibilityPrivate}, "org", slog.Default())
		require.NoError(t, err)

		_, err = collectPages(t, pages)
		require.NoError(t, err)
		require.Equal(t, []string{"/orgs/org/repos?per_page=100&type=sources&page=1"}, *targets)
	})

	t.Run("per page bigger than the API maximum keeps paginating", func(t *testing.T) {
		targets := recordTargets(t, 100, 2)
		pages, err := streamRepoPages(context.Background(), SearchOptions{Page: AllPages, PerPage: 500}, "org", slog.Default())
		require.NoError(t, err)

		got, err := collectPages(t, pages)
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Len(t, *targets, 3)
	})

	t.Run("fetch error is streamed", func(t *testing.T) {
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(context.Context, string, ...string) (string, error) {
					return "", errors.New("boom")
				},
			}
		})
		pages, err := streamRepoPages(context.Background(), SearchOptions{}, "org", slog.Default())
		require.NoError(t, err)

		_, err = collectPages(t, pages)
		require.ErrorContains(t, err, "boom")
	})

	t.Run("invalid options", func(t *testing.T) {
		_, err := streamRepoPages(context.Background(), SearchOptions{PerPage: -1}, "org", slog.Default())
		require.Error(t, err)
		_, err = streamRepoPages(context.Background(), SearchOptions{Page: -2}, "org", slog.Default())
		require.Error(t, err)
	})

	t.Run("stops when the context is done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		recordTargets(t, 2, 1000)
		pages, err := streamRepoPages(ctx, SearchOptions{Page: AllPages, PerPage: 2}, "org", slog.Default())
		require.NoError(t, err)

		<-pages
		cancel()
		// goleak verifies the goroutine exits
	})
}

func TestRepoListType(t *testing.T) {
	require.Equal(t, "", repoListType(SearchOptions{}))
	require.Equal(t, "forks", repoListType(SearchOptions{Source: OnlyForks}))
	require.Equal(t, "sources", repoListType(SearchOptions{Source: OnlyNonForks, Visibility: VisibilityPublic}))
	require.Equal(t, "public", repoListType(SearchOptions{Visibility: VisibilityPublic}))
	require.Equal(t, "private", repoListType(SearchOptions{Visibility: VisibilityPrivate}))
	require.Equal(t, "", repoListType(SearchOptions{Visibility: VisibilityInternal}))
}

func TestPrefetchAndReplayRepoPages(t *testing.T) {
	var pages [][]Repository
	for i := range 4 {
		pages = append(pages, []Repository{{Name: fmt.Sprintf("org/repo-%d", i)}})
	}

	ctx := context.Background()
	stream := pagesChan(pages)

	prefetched, err := prefetchRepoPages(stream, func(Repository) bool { return true })
	require.NoError(t, err)
	require.Len(t, prefetched, maxCloneabilityChecks)

	got, err := collectPages(t, replayRepoPages(ctx, prefetched, stream))
	require.NoError(t, err)
	require.Equal(t, pages, got)
}

func TestRunForReposConcurrentlyStreamError(t *testing.T) {
	pages := make(chan repoPage, 2)
	pages <- repoPage{repos: []Repository{{Name: "repo1"}}}
	pages <- repoPage{err: errors.New("listing failed")}
	close(pages)

	_, err := runForReposConcurrently(context.Background(), pages, 2, func(Repository) bool { return true }, processRepository, func(context.Context, Repository, exec.Execer) error { return nil }, Options{})
	require.ErrorContains(t, err, "listing failed")
}

func TestRunForReposConcurrentlyErrorDoesNotBlockFeeder(t *testing.T) {
	var repos []Repository
	for i := range 100 {
		repos = append(repos, Repository{Name: fmt.Sprintf("repo-%d", i)})
	}

	processorCaller := func(context.Context, Repository, Processor, RunOptions) error {
		return errors.New("always fails")
	}

	// every worker exits on its first error, the feeder must not block forever.
	_, err := runForReposConcurrently(context.Background(), pagesChan([][]Repository{repos}), 2, func(Repository) bool { return true }, processorCaller, nil, Options{})
	require.ErrorContains(t, err, "always fails")
}

func TestCloneDepth(t *testing.T) {
	require.Equal(t, 1, cloneDepth(Options{}))
	require.Equal(t, 5, cloneDepth(Options{CloneDepth: 5}))
	require.Equal(t, 0, cloneDepth(Options{CloneDepth: -1}))
}

func TestCloneRepositoryFetchDepth(t *testing.T) {
	for name, tc := range map[string]struct {
		opts     Options
		expected []string
	}{
		"default": {Options{}, []string{"fetch", "--depth", "1", "origin", "main"}},
		"custom":  {Options{CloneDepth: 10}, []string{"fetch", "--depth", "10", "origin", "main"}},
		"full":    {Options{CloneDepth: -1}, []string{"fetch", "origin", "main"}},
	} {
		t.Run(name, func(t *testing.T) {
			var fetchArgs []string
			overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
				return mock.Execer{
					RunXFn: func(_ context.Context, _ string, args ...string) (string, error) {
						if args[0] == "fetch" {
							fetchArgs = args
						}
						return "", nil
					},
				}
			})

			err := cloneRepository(context.Background(), Repository{DefaultBranchName: "main"}, t.TempDir(), tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expected, fetchArgs)
		})
	}
}

func TestEnsureCloneConcurrentSameDir(t *testing.T) {
	var (
		mu     sync.Mutex
		clones int
	)
	overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
		return mock.Execer{
			RunXFn: func(_ context.Context, _ string, args ...string) (string, error) {
				if args[0] == "init" {
					mu.Lock()
					clones++
					mu.Unlock()
				}
				if args[0] == "fetch" {
					time.Sleep(50 * time.Millisecond) // a slow clone
				}
				return "", nil
			},
		}
	})

	cloneDir := filepath.Join(t.TempDir(), "repo")
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, ensureClone(context.Background(), Repository{DefaultBranchName: "main"}, cloneDir, Options{}))
		}()
	}
	wg.Wait()

	require.Equal(t, 1, clones)
}
