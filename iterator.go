package iterator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	osexec "os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
	"github.com/jcchavezs/gh-iterator/internal/log"
)

// Repository represents a GitHub repository
type Repository struct {
	Name              string    `json:"full_name"`
	URL               string    `json:"clone_url"`
	SSHURL            string    `json:"ssh_url"`
	DefaultBranchName string    `json:"default_branch"`
	Archived          bool      `json:"archived"`
	ArchivedAt        time.Time `json:"archived_at"`
	Language          string    `json:"language"`
	Visibility        string    `json:"visibility"`
	Fork              bool      `json:"fork"`
	Size              int       `json:"size"`
	PushedAt          time.Time `json:"pushed_at"`
}

func (r Repository) IsEmpty() bool {
	return r.Size == 0
}

func (r Repository) String() string {
	return r.Name
}

var (
	baseDir  string
	reposDir string
)

func init() {
	baseDir, _ = filepath.Abs(os.TempDir())
	reposDir = path.Join(baseDir, "gh-iterator")
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		panic(err)
	}
}

// Processor is the function that process a repository.
// - ctx is the context to cancel the processing.
// - repository is the representation of the repository.
// - exec is an exec.Execer to run commands in the repository directory.
type Processor func(ctx context.Context, repository Repository, exec exec.Execer) error

// CloneCacheKey is a function to generate a cache key for a repository clone.
type CloneCacheKey func(repository Repository) string

func CloneCacheKeyFromString(s string) CloneCacheKey {
	return func(Repository) string { return s }
}

// RunOptions are the options to run the iterator.
type RunOptions = Options

// Options are the options to run the iterator.
type Options struct {
	// UseHTTPS is a flag to use HTTPS instead of SSH to clone the repositories.
	UseHTTPS bool
	// CloneCacheKey is a key to identify the cached version of a repository clone to be used
	// in an execution. This is beneficial when during the same execution a repository is cloned
	// many times reducing the execution time by cloning once and copying the same repository locally.
	// If the key is empty, no cache will be used.
	CloneCacheKey CloneCacheKey
	// CloningSubset is a list of files or directories to clone to avoid cloning the whole repository.
	// it is helpful on big repositories to speed up the process.
	CloningSubset []string
	// FetchDepth is the number of commits to fetch, similar to `fetch-depth` in actions/checkout.
	// Defaults to 1 (a shallow clone with only the latest commit) when 0. Set a negative value
	// to fetch the whole history.
	FetchDepth int
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers. Only valid when calling `RunForOrganization``
	NumberOfWorkers int
	// Debug is a flag to print debug information.
	// deprecated
	Debug bool
	// Log handler
	LogHandler slog.Handler
	// ContextEnricher is a function to enrich the context before processing a repository.
	ContextEnricher func(context.Context, Repository) context.Context
}

const (
	defaultNumberOfWorkers = 10
	GithubAPIVersion       = "2022-11-28"
)

// Result holds the result from running the iterator for an organization.
type Result struct {
	// Found is the total number of repositories found i.e. the total number of
	// repositories retrieved from the API.
	Found int
	// Inspected is the total number of repositories inspected before the filtering.
	Inspected int
	// Processed is the total number of repositories processed after the filtering.
	Processed int
}

// RunForOrganization runs the processor for all repositories in an organization. Repositories
// start being processed as soon as the first page is retrieved, without waiting for the rest.
func RunForOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts RunOptions) (Result, error) {
	defer os.RemoveAll(reposDir) //nolint:errcheck

	ctx, logger := setupLogger(ctx, opts.LogHandler, opts.Debug)

	// cancelling stops the retrieval of pages if we return before consuming all of them.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages, err := streamRepoPages(ctx, searchOpts, orgName, logger)
	if err != nil {
		return Result{}, err
	}

	filterIn := searchOpts.MakeFilterIn()
	pages, found, err := checkCloneabilityFromStream(ctx, pages, filterIn, opts.UseHTTPS)
	if err != nil {
		return Result{Found: found}, err
	}

	var nOfWorkers = defaultNumberOfWorkers
	if opts.NumberOfWorkers > 0 {
		nOfWorkers = opts.NumberOfWorkers
	}

	return runForReposConcurrently(ctx, pages, nOfWorkers, filterIn, processRepository, processor, opts)
}

// repoPage is a page of repositories retrieved from the API or the error that
// interrupted the retrieval.
type repoPage struct {
	repos []Repository
	err   error
}

// maxRepoPageSize is the maximum size of a single page (a line of gh's output).
const maxRepoPageSize = 16 << 20

// streamBufferedPages is the number of parsed pages that can wait for the consumer.
const streamBufferedPages = 2

// streamRepoPages retrieves the repositories of an organization and emits each page as soon
// as it is available. The channel is closed when there are no more pages, if there was a
// failure the last emitted page carries the error. Cancelling the context stops the retrieval.
func streamRepoPages(ctx context.Context, searchOpts SearchOptions, orgName string, logger *slog.Logger) (<-chan repoPage, error) {
	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + GithubAPIVersion,
		"-X", "GET",
		"--jq", ". | map({full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size,pushed_at,archived_at})",
	}

	if searchOpts.Cache > 0 {
		ghArgs = append(ghArgs, "--cache", searchOpts.Cache.String())
	}

	target := fmt.Sprintf("/orgs/%s/repos", orgName)
	if searchOpts.PerPage == 0 || searchOpts.PerPage > maxPerPage {
		target = fmt.Sprintf("%s?per_page=%d", target, defaultPerPage)
	} else if searchOpts.PerPage > 0 {
		target = fmt.Sprintf("%s?per_page=%d", target, searchOpts.PerPage)
	} else {
		return nil, errors.New("invalid negative SearchOptions.PerPage")
	}

	if searchOpts.Page == AllPages {
		ghArgs = append(ghArgs, "--paginate")
	} else if searchOpts.Page > 0 {
		target = fmt.Sprintf("%s&page=%d", target, searchOpts.Page)
	} else if searchOpts.Page != 0 {
		return nil, errors.New("invalid negative SearchOptions.Page")
	}

	ghArgs = append(ghArgs, target)

	cmd := osexec.CommandContext(ctx, "gh", ghArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("fetching repositories: %w", err)
	}

	cmdS := strings.Join(append([]string{"gh"}, ghArgs...), " ")
	logger.Debug("Executing command", "command", cmdS)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("fetching repositories: %s: %w", cmdS, err)
	}

	// a small buffer lets the next pages be parsed while the consumer is busy e.g. checking
	// the cloneability, the rest is held back by the pipe so memory stays bounded.
	out := make(chan repoPage, streamBufferedPages)
	send := func(p repoPage) bool {
		select {
		case out <- p:
			return true
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		defer close(out)

		// failure is the payload of a line that is not a page, which for failed
		// requests holds the error returned by the API.
		var (
			failure  []byte
			parseErr error
		)

		sc := bufio.NewScanner(stdout)
		sc.Buffer(nil, maxRepoPageSize)
		for sc.Scan() {
			if len(sc.Bytes()) <= 2 {
				break
			}

			page := []Repository{}
			if err := json.Unmarshal(sc.Bytes(), &page); err != nil {
				parseErr = fmt.Errorf("unmarshaling repositories: %w", err)
				failure = append([]byte(nil), sc.Bytes()...)
				rest, _ := io.ReadAll(stdout)
				failure = append(failure, rest...)
				break
			}

			if !send(repoPage{repos: page}) {
				_ = cmd.Wait()
				return
			}
		}

		scanErr := sc.Err()
		if scanErr != nil {
			scanErr = fmt.Errorf("scanning response pages: %w", scanErr)
		}

		// the output must be fully read before waiting for the command.
		_, _ = io.Copy(io.Discard, stdout)

		if waitErr := cmd.Wait(); waitErr != nil {
			if ctx.Err() != nil {
				return
			}

			var exitErr *osexec.ExitError
			if errors.As(waitErr, &exitErr) {
				waitErr = exec.NewExecErr(fmt.Sprintf("%s: exit code %d", cmdS, exitErr.ExitCode()), stderr.String(), exitErr.ExitCode())
			}

			send(repoPage{err: fmt.Errorf("fetching repositories: %w", github.ErrOrGHAPIErr(string(failure), waitErr))})
			return
		}

		if err := errors.Join(parseErr, scanErr); err != nil {
			send(repoPage{err: fmt.Errorf("processing repositories pages: %w", err)})
		}
	}()

	return out, nil
}

func setupLogger(ctx context.Context, logHandler slog.Handler, debug bool) (context.Context, *slog.Logger) {
	var logger *slog.Logger
	if logHandler != nil {
		logger = slog.New(logHandler)
	} else if debug {
		logger = slog.Default()
	} else {
		logger = slog.New(log.DiscardHandler)
	}

	return log.NewCtx(ctx, logger), logger
}

const maxCloneabilityChecks = 3

// selectCloneabilityCheckCandidates selects at most `maxCloneabilityChecks` repositories that pass the filter to check their cloneability.
func selectCloneabilityCheckCandidates(repoPages [][]Repository, filterIn func(Repository) bool) []Repository {
	var repos = make([]Repository, 0, maxCloneabilityChecks)

	for _, rp := range repoPages {
		for _, r := range rp {
			if filterIn(r) && r.Name != "" {
				repos = append(repos, r)

				if len(repos) == maxCloneabilityChecks {
					return repos
				}
			}
		}
	}

	return repos
}

// checkCloneability tries to clone at most `maxCloneabilityChecks` repositories to ensure
// that the authentication method works correctly.
func checkCloneability(ctx context.Context, repoPages [][]Repository, filterIn func(Repository) bool, useHTTPS bool) error {
	repos := selectCloneabilityCheckCandidates(repoPages, filterIn)

	if len(repos) == 0 {
		return errors.New("no repositories to check cloneability")
	}

	logger := log.FromCtx(ctx)
	xr := newExecerWithLogger(".", logger)

	var errs []error
	for _, repo := range repos {
		logger.Debug("Checking repository cloneability", "repository", repo.Name)

		repoURL := repo.SSHURL
		if useHTTPS {
			repoURL = repo.URL
		}

		_, err := xr.RunX(ctx, "git", "ls-remote", "--exit-code", repoURL)
		if err == nil {
			if len(errs) > 0 {
				logger.Debug("Cloneability check passed after previous failures", "error", errors.Join(errs...))
			}

			return nil
		}

		errs = append(errs, fmt.Errorf("checking repository %q cloneability: %w", repo.Name, err))
	}

	return errors.Join(errs...)
}

// checkCloneabilityFromStream consumes pages only until there are enough candidates to check
// the cloneability and returns a channel that replays them followed by the remaining pages.
// It also returns the number of repositories found while checking.
func checkCloneabilityFromStream(ctx context.Context, pages <-chan repoPage, filterIn func(Repository) bool, useHTTPS bool) (<-chan repoPage, int, error) {
	var (
		buffered   [][]Repository
		candidates int
	)

	for candidates < maxCloneabilityChecks {
		p, ok := <-pages
		if !ok {
			break
		}

		if p.err != nil {
			return nil, countRepoPages(buffered), p.err
		}

		buffered = append(buffered, p.repos)
		candidates += len(selectCloneabilityCheckCandidates([][]Repository{p.repos}, filterIn))
	}

	// a cancelled context closes the stream without an error, which must not be
	// mistaken for an organization with no repositories.
	if err := ctx.Err(); err != nil {
		return nil, countRepoPages(buffered), err
	}

	if err := checkCloneability(ctx, buffered, filterIn, useHTTPS); err != nil {
		return nil, countRepoPages(buffered), err
	}

	replay := make(chan repoPage)
	go func() {
		defer close(replay)

		for _, b := range buffered {
			select {
			case replay <- repoPage{repos: b}:
			case <-ctx.Done():
				return
			}
		}

		for p := range pages {
			select {
			case replay <- p:
			case <-ctx.Done():
				return
			}
		}
	}()

	return replay, countRepoPages(buffered), nil
}

var newExecerWithLogger = exec.NewExecerWithLogger

// countRepoPages counts the total number of repositories in the pages.
func countRepoPages(repoPages [][]Repository) int {
	var mFound int
	for _, repoPage := range repoPages {
		mFound += len(repoPage)
	}
	return mFound
}

// runForReposConcurrently runs the processor for the repositories concurrently using nOfWorkers workers.
func runForReposConcurrently(
	ctx context.Context,
	pages <-chan repoPage,
	nOfWorkers int,
	filterIn func(Repository) bool,
	processorCaller func(context.Context, Repository, Processor, RunOptions) error,
	processor Processor,
	opts RunOptions,
) (Result, error) {
	var (
		mFound, mInspected, mProcessed int
		fetchErr                       error
		repoC                          = make(chan Repository, nOfWorkers)
		errC                           = make(chan error, nOfWorkers)
		doneC                          = make(chan struct{})
		// doneC can be closed by the producer or the collector, whichever comes first.
		closeDone = sync.OnceFunc(func() { close(doneC) })
		wg        = sync.WaitGroup{}
		logger    = log.FromCtx(ctx)
	)

	for range nOfWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range repoC {
				select {
				case <-ctx.Done():
					// if the context is cancelled we do not process any more repositories
					continue
				default:
					if err := processorCaller(ctx, repo, processor, opts); err != nil {
						if errors.Is(err, errNoDefaultBranch) {
							logger.Warn("Repository with no default branch", "repository", repo.Name)
							continue
						}

						errC <- fmt.Errorf("processing %q: %w", repo.Name, err)
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(repoC)
		for {
			var (
				page repoPage
				ok   bool
			)

			select {
			case page, ok = <-pages:
			case <-ctx.Done():
				return
			case <-doneC:
				return
			}

			if !ok {
				break
			}

			if page.err != nil {
				fetchErr = page.err
				closeDone()
				return
			}

			// the counters are only written by this goroutine and read once it is done.
			mFound += len(page.repos)

			for _, repo := range page.repos {
				mInspected++
				if !filterIn(repo) {
					continue
				}
				mProcessed++

				// Sending must also be cancellable, otherwise the producer blocks forever
				// if all the workers have exited after an error.
				select {
				case repoC <- repo:
				case <-ctx.Done():
					return
				case <-doneC:
					return
				}
			}
		}
		closeDone()
	}()

	for {
		select {
		case err, ok := <-errC:
			if ok {
				closeDone()
				wg.Wait()
				close(errC)
				return Result{}, err
			}
		case <-ctx.Done():
			closeDone()
			wg.Wait()
			close(errC)
			return Result{}, ctx.Err()
		case <-doneC:
			wg.Wait()
			defer close(errC)

			select {
			case err := <-errC:
				return Result{}, err
			default:
				if fetchErr != nil {
					return Result{}, fetchErr
				}

				// the producer may have finished while the context was cancelled, in
				// which case workers skipped repositories and the run is incomplete.
				if err := ctx.Err(); err != nil {
					return Result{}, err
				}

				return Result{mFound, mInspected, mProcessed}, nil
			}
		}
	}
}

// RunForRepository runs the processor for a single repository.
func RunForRepository(ctx context.Context, repoName string, processor Processor, opts RunOptions) error {
	if strings.Count(repoName, "/") > 1 {
		return fmt.Errorf("incorrect repository name %q", repoName)
	}

	ctx, logger := setupLogger(ctx, opts.LogHandler, opts.Debug)

	x := exec.NewExecerWithLogger(".", logger)

	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + GithubAPIVersion,
		"-X", "GET",
		"--jq", "{full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size,pushed_at,archived_at}",
		fmt.Sprintf("/repos/%s", repoName),
	}

	res, err := x.RunX(ctx, "gh", ghArgs...)
	if err != nil {
		return fmt.Errorf("fetching repository %q: %w", repoName, github.ErrOrGHAPIErr(res, err))
	}

	repo := Repository{}
	err = json.Unmarshal([]byte(res), &repo)
	if err != nil {
		return fmt.Errorf("unmarshaling repository: %w", err)
	}

	if err = processRepository(ctx, repo, processor, opts); err != nil {
		return fmt.Errorf("processing %q: %w", repo.Name, err)
	}

	return nil
}

var (
	errNoDefaultBranch = errors.New("no default branch")
)

// hashCloningSubset generates a hash for the cloning subset to be used as part of the cache key when cloning repositories with a subset of files or directories.
func hashCloningSubset(cs []string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(strings.Join(cs, "|"))))[:16]
}

// cloneDirName builds the name of the directory holding a clone. Clones with different
// subsets or fetch depths must not share a directory. The subset and depth segments are
// always present so the name can be parsed unambiguously from the right, regardless of
// what the (arbitrary) cache key contains.
func cloneDirName(repo Repository, cacheKey string, opts RunOptions) string {
	subset := "none"
	if len(opts.CloningSubset) > 0 {
		subset = hashCloningSubset(opts.CloningSubset)
	}

	return fmt.Sprintf("%s-%s-subset%s-depth%d", repo.Name, cacheKey, subset, effectiveFetchDepth(opts.FetchDepth))
}

// cloneGates holds one single-slot gate per cache directory.
var cloneGates sync.Map

// lockClone acquires the gate of the cache directory or gives up if the context is
// done first, so a cancelled caller doesn't wait for an unrelated slow clone.
func lockClone(ctx context.Context, cloneDir string) (unlock func(), err error) {
	g, _ := cloneGates.LoadOrStore(cloneDir, make(chan struct{}, 1))
	gate := g.(chan struct{})

	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func cloneRepositoryOrGetFromCache(ctx context.Context, repo Repository, opts RunOptions) (string, error) {
	logger := log.FromCtx(ctx)

	var (
		cacheKey             string
		shouldReturnDirectly bool
	)

	if opts.CloneCacheKey != nil {
		cacheKey = opts.CloneCacheKey(repo)
	}

	if cacheKey == "" {
		shouldReturnDirectly = true
		cacheKey = time.Now().Format("02T15_04_05.999999999")
	}

	cloneDir := path.Join(reposDir, cloneDirName(repo, cacheKey, opts))

	ensureClone := func() error {
		if !shouldReturnDirectly {
			// Workers sharing a cache key must not race to create the same clone, nor
			// copy it while it is still being cloned.
			unlock, err := lockClone(ctx, cloneDir)
			if err != nil {
				return fmt.Errorf("waiting for cloning directory: %w", err)
			}
			defer unlock()
		}

		if cloneDirInfo, err := os.Stat(cloneDir); err == nil {
			if !cloneDirInfo.IsDir() {
				return fmt.Errorf("unexpected file in cloning directory: %s", cloneDir)
			}
		} else if os.IsNotExist(err) {
			if err := os.MkdirAll(cloneDir, os.ModePerm); err != nil {
				return fmt.Errorf("creating cloning directory: %w", err)
			}

			if err := cloneRepository(ctx, repo, cloneDir, opts); err != nil {
				if rErr := os.RemoveAll(cloneDir); rErr != nil {
					logger.Warn("Failed to remove the clone directory", "error", rErr)
				}

				return err
			}
		} else {
			return fmt.Errorf("checking clone directory: %w", err)
		}

		return nil
	}

	if err := ensureClone(); err != nil {
		return "", err
	}

	if shouldReturnDirectly {
		return cloneDir, nil
	}

	xr := newExecerWithLogger(reposDir, logger)
	repoDir := cloneDir + "_" + randSequence(9)

	if _, err := xr.RunX(ctx, "cp", copyArgs(runtime.GOOS, cloneDir, repoDir)...); err != nil {
		if rErr := os.RemoveAll(repoDir); rErr != nil {
			logger.Warn("Failed to remove the repo directory", "error", rErr)
		}
		return "", fmt.Errorf("copying repository: %w", err)
	}

	return repoDir, nil
}

// effectiveFetchDepth resolves the default depth: 0 means 1, negative means full history (0 in git terms).
func effectiveFetchDepth(depth int) int {
	switch {
	case depth == 0:
		return 1
	case depth < 0:
		return 0
	}
	return depth
}

// copyArgs returns the cp arguments to copy a directory, using copy-on-write
// when the platform supports it (falling back to a regular copy otherwise).
func copyArgs(goos, src, dst string) []string {
	switch goos {
	case "darwin":
		return []string{"-cR", src, dst}
	case "linux":
		return []string{"-r", "--reflink=auto", src, dst}
	}

	return []string{"-r", src, dst}
}

// fetchArgs returns the git fetch arguments for the given depth and branch. When
// blobless is true, blobs are not downloaded upfront but lazily on checkout.
func fetchArgs(depth int, branch string, blobless bool) []string {
	args := []string{"fetch", "--no-tags"}
	if blobless {
		args = append(args, "--filter=blob:none")
	}

	if d := effectiveFetchDepth(depth); d > 0 {
		args = append(args, "--depth="+strconv.Itoa(d))
	}

	return append(args, "origin", branch)
}

// cloneRepository clones the repository to the given directory.
func cloneRepository(ctx context.Context, repo Repository, repoDir string, opts RunOptions) error {
	logger := log.FromCtx(ctx)

	xr := newExecerWithLogger(repoDir, logger)

	if _, err := xr.RunX(ctx, "git", "init"); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}

	repoURL := repo.SSHURL
	if opts.UseHTTPS {
		repoURL = repo.URL
	}

	if _, err := xr.RunX(ctx, "git", "remote", "add", "origin", repoURL); err != nil {
		return fmt.Errorf("adding origin: %w", err)
	}

	if len(opts.CloningSubset) > 0 {
		if _, err := xr.RunX(ctx, "git", "config", "core.sparseCheckout", "true"); err != nil {
			return fmt.Errorf("setting sparse checkout subset: %w", err)
		}

		logger.Debug("Sparse checkout", "subset", opts.CloningSubset)
		if err := fillLines(filepath.Join(repoDir, ".git/info/sparse-checkout"), opts.CloningSubset); err != nil {
			return fmt.Errorf("setting cloning subset: %w", err)
		}
	}

	if repo.DefaultBranchName == "" {
		return errNoDefaultBranch
	}

	if _, err := xr.RunX(ctx, "git", fetchArgs(opts.FetchDepth, repo.DefaultBranchName, len(opts.CloningSubset) > 0)...); err != nil {
		return fmt.Errorf("fetching HEAD: %w", err)
	}

	if _, err := xr.RunX(ctx, "git", "checkout", repo.DefaultBranchName); err != nil {
		return fmt.Errorf("checking out HEAD: %w", err)
	}

	return nil
}

func processRepository(ctx context.Context, repo Repository, processor Processor, opts RunOptions) error {
	logger := log.FromCtx(ctx).With("repository", repo.Name)

	processCtx := log.NewCtx(ctx, logger)
	if opts.ContextEnricher != nil {
		processCtx = opts.ContextEnricher(processCtx, repo)
	}

	if repo.Size == 0 {
		logger.Debug("Empty repository")

		if err := processor(processCtx, repo, exec.NewExecer("").WithEnv("GH_REPO", repo.Name)); err != nil {
			return fmt.Errorf("processing empty repository: %w", err)
		}

		// no need to clone an empty repository
		return nil
	}

	repoDir, err := cloneRepositoryOrGetFromCache(processCtx, repo, opts)
	if err != nil {
		return err
	}
	defer os.RemoveAll(repoDir) //nolint:errcheck

	if err := processor(processCtx, repo, exec.NewExecerWithLogger(repoDir, logger)); err != nil {
		return err
	}

	return nil
}

// fillLines writes the lines to a file.
func fillLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	for _, l := range lines {
		if _, err := fmt.Fprintln(f, l); err != nil {
			return fmt.Errorf("printing lines to file: %w", err)
		}
	}

	return nil
}
