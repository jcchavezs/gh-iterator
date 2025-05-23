package iterator

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
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

	processor := func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
		processedMux.Lock()
		defer processedMux.Unlock()
		processedRepos = append(processedRepos, repository)
		return nil
	}

	opts := Options{
		Debug: true,
	}

	result, err := runForReposConcurrently(ctx, repoPages, nOfWorkers, func(repo Repository) bool { return true }, processor, opts)
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

	processor := func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
		processedMux.Lock()
		defer processedMux.Unlock()
		processedRepos = append(processedRepos, repository)
		return nil
	}

	opts := Options{
		Debug: true,
	}

	filterIn := func(repo Repository) bool {
		return repo.Language == "Go"
	}

	result, err := runForReposConcurrently(ctx, repoPages, nOfWorkers, filterIn, processor, opts)
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
	processor := func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
		return nil
	}

	opts := Options{
		Debug: true,
	}

	result, err := runForReposConcurrently(ctx, repoPages, nOfWorkers, func(repo Repository) bool { return true }, processor, opts)
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
	processor := func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
		cancel() // Cancel the context during processing
		return nil
	}

	opts := Options{
		Debug: true,
	}

	_, err := runForReposConcurrently(ctx, repoPages, nOfWorkers, func(repo Repository) bool { return true }, processor, opts)

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
	processor := func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
		if repository == "repo2" {
			return fmt.Errorf("error processing %s", repository)
		}
		return nil
	}

	opts := Options{
		Debug: true,
	}

	_, err := runForReposConcurrently(ctx, repoPages, nOfWorkers, func(repo Repository) bool { return true }, processor, opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "error processing repo2")
}
