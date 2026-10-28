package iterator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/jcchavezs/gh-iterator/internal/log"
	"github.com/stretchr/testify/require"
)

// fakeGH installs a `gh` executable in PATH running the given shell script body.
func fakeGH(t *testing.T, body string) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func drain(t *testing.T, c <-chan repoPage) ([][]Repository, error) {
	t.Helper()

	var pages [][]Repository
	timeout := time.After(10 * time.Second)
	for {
		select {
		case p, ok := <-c:
			if !ok {
				return pages, nil
			}
			if p.err != nil {
				return pages, p.err
			}
			pages = append(pages, p.repos)
		case <-timeout:
			t.Fatal("timeout waiting for pages")
		}
	}
}

func TestStreamRepoPages(t *testing.T) {
	logger := slog.New(log.DiscardHandler)

	t.Run("invalid options", func(t *testing.T) {
		_, err := streamRepoPages(context.Background(), SearchOptions{PerPage: -1}, "org", logger)
		require.EqualError(t, err, "invalid negative SearchOptions.PerPage")

		_, err = streamRepoPages(context.Background(), SearchOptions{Page: -5}, "org", logger)
		require.EqualError(t, err, "invalid negative SearchOptions.Page")
	})

	t.Run("gh not found", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := streamRepoPages(context.Background(), SearchOptions{}, "org", logger)
		require.ErrorContains(t, err, "fetching repositories")
	})

	t.Run("emits every page", func(t *testing.T) {
		fakeGH(t, `echo '[{"full_name":"org/a"},{"full_name":"org/b"}]'; echo '[{"full_name":"org/c"}]'`)

		c, err := streamRepoPages(context.Background(), SearchOptions{Page: AllPages}, "org", logger)
		require.NoError(t, err)

		pages, err := drain(t, c)
		require.NoError(t, err)
		require.Equal(t, [][]Repository{
			{{Name: "org/a"}, {Name: "org/b"}},
			{{Name: "org/c"}},
		}, pages)
	})

	t.Run("passes the expected arguments", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args")
		fakeGH(t, fmt.Sprintf(`echo "$@" > %q; echo '[]'`, argsFile))

		c, err := streamRepoPages(context.Background(), SearchOptions{Page: AllPages, PerPage: 20}, "my-org", logger)
		require.NoError(t, err)
		_, err = drain(t, c)
		require.NoError(t, err)

		args, err := os.ReadFile(argsFile)
		require.NoError(t, err)
		require.Contains(t, string(args), "--paginate")
		require.Contains(t, string(args), "/orgs/my-org/repos?per_page=20")
	})

	t.Run("stops at an empty page", func(t *testing.T) {
		fakeGH(t, `echo '[{"full_name":"org/a"}]'; echo '[]'; echo '[{"full_name":"org/ignored"}]'`)

		c, err := streamRepoPages(context.Background(), SearchOptions{}, "org", logger)
		require.NoError(t, err)

		pages, err := drain(t, c)
		require.NoError(t, err)
		require.Equal(t, [][]Repository{{{Name: "org/a"}}}, pages)
	})

	t.Run("emits earlier pages before a failure", func(t *testing.T) {
		fakeGH(t, `echo '[{"full_name":"org/a"}]'; echo "boom" >&2; exit 3`)

		c, err := streamRepoPages(context.Background(), SearchOptions{}, "org", logger)
		require.NoError(t, err)

		pages, err := drain(t, c)
		require.Equal(t, [][]Repository{{{Name: "org/a"}}}, pages)
		require.ErrorContains(t, err, "fetching repositories")
		require.ErrorContains(t, err, "exit code 3")

		stderr, ok := exec.GetStderr(err)
		require.True(t, ok)
		require.Contains(t, stderr, "boom")
	})

	t.Run("api error payload", func(t *testing.T) {
		fakeGH(t, `echo '{"message":"Not Found","status":"404"}'; exit 1`)

		c, err := streamRepoPages(context.Background(), SearchOptions{}, "org", logger)
		require.NoError(t, err)

		pages, err := drain(t, c)
		require.Empty(t, pages)
		require.ErrorContains(t, err, "status 404")
	})

	t.Run("malformed page", func(t *testing.T) {
		fakeGH(t, `echo 'this is not json'`)

		c, err := streamRepoPages(context.Background(), SearchOptions{}, "org", logger)
		require.NoError(t, err)

		_, err = drain(t, c)
		require.ErrorContains(t, err, "unmarshaling repositories")
	})

	t.Run("cancelling stops the command", func(t *testing.T) {
		fakeGH(t, `echo '[{"full_name":"org/a"}]'; exec sleep 30`)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		c, err := streamRepoPages(ctx, SearchOptions{}, "org", logger)
		require.NoError(t, err)

		p := <-c
		require.NoError(t, p.err)
		require.Equal(t, []Repository{{Name: "org/a"}}, p.repos)

		cancel()

		select {
		case _, ok := <-c:
			require.False(t, ok, "no more pages expected")
		case <-time.After(10 * time.Second):
			t.Fatal("stream was not closed after cancelling")
		}
	})
}

func pagesChan(items ...repoPage) <-chan repoPage {
	c := make(chan repoPage, len(items))
	for _, i := range items {
		c <- i
	}
	close(c)
	return c
}

func TestCheckCloneabilityFromStream(t *testing.T) {
	ctx := context.Background()
	acceptAll := func(Repository) bool { return true }

	repo := func(name string) Repository {
		return Repository{Name: name, SSHURL: "git@github.com:" + name + ".git"}
	}

	okExecer := func(t *testing.T, calls *int) {
		t.Helper()
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(context.Context, string, ...string) (string, error) {
					*calls++
					return "", nil
				},
			}
		})
	}

	t.Run("replays every page in order", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		in := [][]Repository{
			{repo("o/1")},
			{repo("o/2")},
			{repo("o/3")},
			{repo("o/4")},
			{repo("o/5")},
		}

		out, found, err := checkCloneabilityFromStream(ctx, staticPages(in), acceptAll, false)
		require.NoError(t, err)
		require.Equal(t, 3, found, "only the pages needed for the check are consumed upfront")
		require.Equal(t, 1, calls)

		pages, err := drain(t, out)
		require.NoError(t, err)
		require.Equal(t, in, pages)
	})

	t.Run("does not read more pages than needed", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		in := make(chan repoPage) // unbuffered: a read beyond the needed pages would block
		go func() {
			in <- repoPage{repos: []Repository{repo("o/1"), repo("o/2"), repo("o/3")}}
		}()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel() // releases the replay goroutine, nobody consumes out

		out, found, err := checkCloneabilityFromStream(ctx, in, acceptAll, false)
		require.NoError(t, err)
		require.Equal(t, 3, found)
		require.NotNil(t, out)
	})

	t.Run("pages without candidates are buffered until there are some", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		filterIn := func(r Repository) bool { return r.Language == "Go" }
		in := [][]Repository{
			{{Name: "o/1", Language: "Python"}},
			{{Name: "o/2", Language: "Go"}, {Name: "o/3", Language: "Go"}, {Name: "o/4", Language: "Go"}},
			{{Name: "o/5", Language: "Go"}},
		}

		out, found, err := checkCloneabilityFromStream(ctx, staticPages(in), filterIn, false)
		require.NoError(t, err)
		require.Equal(t, 4, found)

		pages, err := drain(t, out)
		require.NoError(t, err)
		require.Equal(t, in, pages)
	})

	t.Run("stream ends with fewer candidates than the maximum", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		in := [][]Repository{{repo("o/1")}}
		out, found, err := checkCloneabilityFromStream(ctx, staticPages(in), acceptAll, false)
		require.NoError(t, err)
		require.Equal(t, 1, found)

		pages, err := drain(t, out)
		require.NoError(t, err)
		require.Equal(t, in, pages)
	})

	t.Run("context cancelled before the first page", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		ctx, cancel := context.WithCancel(ctx)
		cancel()

		// a cancelled stream is closed without emitting an error.
		_, _, err := checkCloneabilityFromStream(ctx, staticPages(nil), acceptAll, false)
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, calls)
	})

	t.Run("empty stream", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		_, found, err := checkCloneabilityFromStream(ctx, staticPages(nil), acceptAll, false)
		require.EqualError(t, err, "no repositories to check cloneability")
		require.Zero(t, found)
		require.Zero(t, calls)
	})

	t.Run("stream error before candidates", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		streamErr := errors.New("api down")
		_, found, err := checkCloneabilityFromStream(ctx, pagesChan(
			repoPage{repos: []Repository{repo("o/1")}},
			repoPage{err: streamErr},
		), acceptAll, false)
		require.ErrorIs(t, err, streamErr)
		require.Equal(t, 1, found)
		require.Zero(t, calls)
	})

	t.Run("failing check", func(t *testing.T) {
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(context.Context, string, ...string) (string, error) {
					return "", errors.New("permission denied")
				},
			}
		})

		out, found, err := checkCloneabilityFromStream(ctx, staticPages([][]Repository{{repo("o/1"), repo("o/2")}}), acceptAll, false)
		require.ErrorContains(t, err, "permission denied")
		require.Nil(t, out)
		require.Equal(t, 2, found)
	})

	t.Run("later stream errors are forwarded", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		streamErr := errors.New("page 2 failed")
		out, _, err := checkCloneabilityFromStream(ctx, pagesChan(
			repoPage{repos: []Repository{repo("o/1"), repo("o/2"), repo("o/3")}},
			repoPage{err: streamErr},
		), acceptAll, false)
		require.NoError(t, err)

		pages, err := drain(t, out)
		require.Len(t, pages, 1)
		require.ErrorIs(t, err, streamErr)
	})

	t.Run("cancelling releases the replay goroutine", func(t *testing.T) {
		var calls int
		okExecer(t, &calls)

		ctx, cancel := context.WithCancel(ctx)
		out, _, err := checkCloneabilityFromStream(ctx, staticPages([][]Repository{{repo("o/1")}, {repo("o/2")}}), acceptAll, false)
		require.NoError(t, err)

		cancel()

		// nobody consumes the channel, it must still be closed.
		require.Eventually(t, func() bool {
			select {
			case _, ok := <-out:
				return !ok
			default:
				return false
			}
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// gatedGH installs a fake `gh` that emits a first page, then waits for the marker file to
// exist before emitting the second one. If the consumer waits for all the pages before
// processing, the marker never appears and gh fails.
func gatedGH(t *testing.T, marker string) {
	t.Helper()

	fakeGH(t, fmt.Sprintf(`echo '[{"full_name":"org/a"},{"full_name":"org/b"},{"full_name":"org/c"}]'
i=0
while [ ! -e %[1]q ]; do
  i=$((i+1))
  [ $i -gt 100 ] && echo "marker never created" >&2 && exit 1
  sleep 0.1
done
echo '[{"full_name":"org/d"}]'`, marker))
}

func TestRunForOrganizationStartsProcessingBeforeAllPagesArrive(t *testing.T) {
	mockLSRemoteCheck(t)

	marker := filepath.Join(t.TempDir(), "marker")
	gatedGH(t, marker)

	var (
		mux       sync.Mutex
		processed []string
	)

	processor := func(_ context.Context, repository Repository, _ exec.Execer) error {
		// the first page is being processed while gh is still waiting to send the second.
		require.NoError(t, os.WriteFile(marker, nil, 0o600))

		mux.Lock()
		defer mux.Unlock()
		processed = append(processed, repository.Name)
		return nil
	}

	res, err := RunForOrganization(context.Background(), "org", SearchOptions{Page: AllPages}, processor, Options{})
	require.NoError(t, err)
	require.Equal(t, Result{Found: 4, Inspected: 4, Processed: 4}, res)
	require.ElementsMatch(t, []string{"org/a", "org/b", "org/c", "org/d"}, processed)
}

func TestViewRepositoriesInOrganizationStartsProcessingBeforeAllPagesArrive(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	gatedGH(t, marker)

	var (
		mux    sync.Mutex
		listed []string
	)

	res, err := ViewRepositoriesInOrganization(context.Background(), "org", SearchOptions{Page: AllPages},
		func(_ context.Context, repository Repository, _ exec.Execer) error {
			require.NoError(t, os.WriteFile(marker, nil, 0o600))

			mux.Lock()
			defer mux.Unlock()
			listed = append(listed, repository.Name)
			return nil
		}, ViewRepositoriesOptions{})
	require.NoError(t, err)
	require.Equal(t, Result{Found: 4, Inspected: 4, Processed: 4}, res)
	require.ElementsMatch(t, []string{"org/a", "org/b", "org/c", "org/d"}, listed)
}

func TestRunForOrganizationFailureAfterFirstPage(t *testing.T) {
	mockLSRemoteCheck(t)
	fakeGH(t, `echo '[{"full_name":"org/a"},{"full_name":"org/b"},{"full_name":"org/c"}]'; echo "rate limited" >&2; exit 1`)

	_, err := RunForOrganization(context.Background(), "org", SearchOptions{Page: AllPages},
		func(context.Context, Repository, exec.Execer) error { return nil }, Options{})
	require.ErrorContains(t, err, "fetching repositories")

	stderr, ok := exec.GetStderr(err)
	require.True(t, ok)
	require.Contains(t, stderr, "rate limited")
}

func TestRunForOrganizationProcessorErrorStopsGH(t *testing.T) {
	mockLSRemoteCheck(t)
	// gh would keep running for a long time: returning must not wait for it.
	fakeGH(t, `echo '[{"full_name":"org/a"},{"full_name":"org/b"},{"full_name":"org/c"}]'; exec sleep 60`)

	processorErr := errors.New("processor failed")
	done := make(chan error, 1)
	go func() {
		_, err := RunForOrganization(context.Background(), "org", SearchOptions{Page: AllPages},
			func(context.Context, Repository, exec.Execer) error { return processorErr }, Options{})
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, processorErr)
	case <-time.After(10 * time.Second):
		t.Fatal("RunForOrganization did not return after a processor error")
	}
}

func TestRunForReposConcurrentlyStreamErrorMidway(t *testing.T) {
	var (
		mux       sync.Mutex
		processed []string
	)

	processor := func(_ context.Context, repository Repository, _ exec.Execer) error {
		mux.Lock()
		defer mux.Unlock()
		processed = append(processed, repository.Name)
		return nil
	}

	streamErr := errors.New("page 2 failed")
	_, err := runForReposConcurrently(context.Background(), pagesChan(
		repoPage{repos: []Repository{{Name: "org/a"}}},
		repoPage{err: streamErr},
	), 2, func(Repository) bool { return true }, processRepository, processor, Options{})
	require.ErrorIs(t, err, streamErr)
}

func TestRunForReposConcurrentlyProcessorErrorWithOpenStream(t *testing.T) {
	// the stream never delivers more nor closes, the producer must not keep the run alive.
	pages := make(chan repoPage, 1)
	pages <- repoPage{repos: []Repository{{Name: "org/a"}}}

	processorErr := errors.New("processor failed")
	done := make(chan error, 1)
	go func() {
		_, err := runForReposConcurrently(context.Background(), pages, 1, func(Repository) bool { return true }, processRepository,
			func(context.Context, Repository, exec.Execer) error { return processorErr }, Options{})
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, processorErr)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return with an open stream")
	}
}

func TestRunForOrganizationContextCancelledBeforeFirstPage(t *testing.T) {
	mockLSRemoteCheck(t)
	fakeGH(t, `exec sleep 60`)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := RunForOrganization(ctx, "org", SearchOptions{Page: AllPages},
		func(context.Context, Repository, exec.Execer) error { return nil }, Options{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
