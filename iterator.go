package iterator

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// IsEmpty reports whether the repository has no content (size 0).
func (r Repository) IsEmpty() bool {
	return r.Size == 0
}

// String returns the full name (owner/repo).
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
// - xr is an exec.Execer to run commands. With the Run* functions it runs in the clone,
// with the View* functions there is no clone and `GH_REPO` is set to the repository.
// Returning an error aborts the whole run.
type Processor func(ctx context.Context, repository Repository, xr exec.Execer) error

// CloneCacheKey is a function to generate a cache key for a repository clone.
type CloneCacheKey func(repository Repository) string

// CloneCacheKeyFromString returns a CloneCacheKey that always returns s.
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
	// CloneDepth is the number of commits to fetch when cloning a repository. By default (0) it
	// fetches only the latest commit (depth 1), a negative value fetches the whole history.
	CloneDepth int
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers. Not used by RunForRepository.
	NumberOfWorkers int
	// Debug is a flag to print debug information.
	//
	// Deprecated: use LogHandler.
	Debug bool
	// Log handler
	LogHandler slog.Handler
	// ContextEnricher is a function to enrich the context before processing a repository.
	ContextEnricher func(context.Context, Repository) context.Context
}

const (
	defaultNumberOfWorkers = 10
	defaultCloneDepth      = 1
	// GithubAPIVersion is the GitHub REST API version sent in API requests.
	GithubAPIVersion = "2022-11-28"
)

// processRepoPages processes the repository pages returned by the GitHub API and returns a slice of repositories.
func processRepoPages(payload string) ([][]Repository, error) {
	ls := bufio.NewScanner(strings.NewReader(payload))
	ls.Split(bufio.ScanLines)
	var repoPages [][]Repository
	for ls.Scan() {
		if len(ls.Bytes()) <= 2 {
			break
		}

		var page = []Repository{}
		if err := json.Unmarshal(ls.Bytes(), &page); err != nil {
			return nil, fmt.Errorf("unmarshaling repositories: %w", err)
		}
		repoPages = append(repoPages, page)
	}

	if err := ls.Err(); err != nil {
		return nil, fmt.Errorf("scaning reponse pages: %w", err)
	}

	return repoPages, nil
}

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

// RunForOrganization runs the processor concurrently for the matching repositories of an
// organization, cloning each one first. Use ViewRepositoriesInOrganization if no clone is needed.
//
// Repositories are processed while the listing is still being fetched, so Result.Found only
// reflects the repositories retrieved before the run ended.
func RunForOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts RunOptions) (Result, error) {
	defer os.RemoveAll(reposDir) //nolint:errcheck

	ctx, logger := setupLogger(ctx, opts.LogHandler, opts.Debug)

	// cancelling stops the listing if the run ends before all pages are consumed.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages, err := streamRepoPages(ctx, searchOpts, orgName, logger)
	if err != nil {
		return Result{}, err
	}

	filterIn := searchOpts.MakeFilterIn()

	// the cloneability check only needs the first matching repositories, so the
	// rest of the listing keeps streaming while the check runs.
	prefetched, err := prefetchRepoPages(pages, filterIn)
	if err != nil {
		return Result{}, err
	}

	if err := checkCloneability(ctx, prefetched, filterIn, opts.UseHTTPS); err != nil {
		return Result{Found: countRepoPages(prefetched)}, err
	}

	// We use half of the default number of workers unless specified otherwise.
	// because running too many concurrent clones can end up in resource exhaustion or rate limiting.
	var nOfWorkers = defaultNumberOfWorkers / 2
	if opts.NumberOfWorkers > 0 {
		nOfWorkers = opts.NumberOfWorkers
	}

	return runForReposConcurrently(ctx, replayRepoPages(ctx, prefetched, pages), nOfWorkers, filterIn, processRepository, processor, opts)
}

// repoPage is a page of repositories or the error that interrupted the listing.
type repoPage struct {
	repos []Repository
	err   error
}

// apiMaxPerPage is the maximum page size honored by the GitHub API.
const apiMaxPerPage = 100

// repoListType returns the value of the `type` query parameter that lets the GitHub API
// discard repositories server-side. The API accepts a single type, so the most selective
// criteria is pushed down and the rest is left to SearchOptions.MakeFilterIn.
func repoListType(so SearchOptions) string {
	switch so.Source {
	case OnlyForks:
		return "forks"
	case OnlyNonForks:
		return "sources"
	}

	switch so.Visibility {
	case VisibilityPublic:
		return "public"
	case VisibilityPrivate:
		return "private"
	}

	return ""
}

// streamRepoPages fetches the repositories of an organization page by page in the background.
// The returned channel is closed once the listing is over, failed or ctx is done.
func streamRepoPages(ctx context.Context, searchOpts SearchOptions, orgName string, logger *slog.Logger) (<-chan repoPage, error) {
	perPage := searchOpts.PerPage
	if perPage < 0 {
		return nil, errors.New("invalid negative SearchOptions.PerPage")
	} else if perPage == 0 || perPage > maxPerPage {
		perPage = defaultPerPage
	}

	if searchOpts.Page < 0 && searchOpts.Page != AllPages {
		return nil, errors.New("invalid negative SearchOptions.Page")
	}

	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + GithubAPIVersion,
		"-X", "GET",
		"--jq", ". | map({full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size,pushed_at,archived_at})",
	}

	if searchOpts.Cache > 0 {
		ghArgs = append(ghArgs, "--cache", searchOpts.Cache.String())
	}

	query := fmt.Sprintf("per_page=%d", perPage)
	if t := repoListType(searchOpts); t != "" {
		query += "&type=" + t
	}

	xr := newExecerWithLogger(".", logger)

	fetchPage := func(page int) ([]Repository, error) {
		target := fmt.Sprintf("/orgs/%s/repos?%s&page=%d", orgName, query, page)
		res, err := xr.RunX(ctx, "gh", append(slices.Clone(ghArgs), target)...)
		if err != nil {
			return nil, fmt.Errorf("fetching repositories: %w", github.ErrOrGHAPIErr(res, err))
		}

		pages, err := processRepoPages(res)
		if err != nil {
			return nil, fmt.Errorf("processing repositories pages: %w", err)
		}

		return slices.Concat(pages...), nil
	}

	var (
		firstPage = max(int(searchOpts.Page), 1)
		allPages  = searchOpts.Page == AllPages
		// a page shorter than this is the last one.
		fullPage = min(perPage, apiMaxPerPage)
		pagesC   = make(chan repoPage, 1)
	)

	send := func(p repoPage) bool {
		select {
		case pagesC <- p:
			return true
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		defer close(pagesC)

		for page := firstPage; ; page++ {
			repos, err := fetchPage(page)
			if err != nil {
				send(repoPage{err: err})
				return
			}

			if len(repos) > 0 && !send(repoPage{repos: repos}) {
				return
			}

			if !allPages || len(repos) < fullPage {
				return
			}
		}
	}()

	return pagesC, nil
}

// prefetchRepoPages reads pages from the stream until there are enough repositories passing
// filterIn to check their cloneability or the stream ends.
func prefetchRepoPages(pages <-chan repoPage, filterIn func(Repository) bool) ([][]Repository, error) {
	var prefetched [][]Repository
	for p := range pages {
		if p.err != nil {
			return nil, p.err
		}

		prefetched = append(prefetched, p.repos)
		if len(selectCloneabilityCheckCandidates(prefetched, filterIn)) == maxCloneabilityChecks {
			break
		}
	}

	return prefetched, nil
}

// replayRepoPages returns a stream with the prefetched pages followed by the rest of the pages.
func replayRepoPages(ctx context.Context, prefetched [][]Repository, rest <-chan repoPage) <-chan repoPage {
	out := make(chan repoPage, 1)

	go func() {
		defer close(out)

		send := func(p repoPage) bool {
			select {
			case out <- p:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for _, repos := range prefetched {
			if !send(repoPage{repos: repos}) {
				return
			}
		}

		for p := range rest {
			if !send(p) {
				return
			}
		}
	}()

	return out
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

var newExecerWithLogger = exec.NewExecerWithLogger

// countRepoPages counts the total number of repositories in the pages.
func countRepoPages(repoPages [][]Repository) int {
	var mFound int
	for _, repoPage := range repoPages {
		mFound += len(repoPage)
	}
	return mFound
}

// runForReposConcurrently runs the processor for the repositories of the pages concurrently using nOfWorkers workers.
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
		repoC    = make(chan Repository)
		stop     = make(chan struct{})
		stopOnce sync.Once
		firstErr error
		result   Result
		wg       sync.WaitGroup
		logger   = log.FromCtx(ctx)
	)

	// fail records the first error and stops feeding workers.
	fail := func(err error) {
		stopOnce.Do(func() {
			firstErr = err
			close(stop)
		})
	}

	for range nOfWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range repoC {
				// if the context is cancelled or the run failed we do not process any more repositories
				if ctx.Err() != nil {
					continue
				}

				select {
				case <-stop:
					continue
				default:
				}

				if err := processorCaller(ctx, repo, processor, opts); err != nil {
					if errors.Is(err, errNoDefaultBranch) {
						logger.Warn("Repository with no default branch", "repository", repo.Name)
						continue
					}

					fail(fmt.Errorf("processing %q: %w", repo.Name, err))
					return
				}
			}
		}()
	}

	// the feeder is the only writer of result.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(repoC)

		for {
			var (
				page repoPage
				ok   bool
			)

			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case page, ok = <-pages:
				if !ok {
					return
				}
			}

			if page.err != nil {
				fail(page.err)
				return
			}

			result.Found += len(page.repos)

			for _, repo := range page.repos {
				result.Inspected++
				if !filterIn(repo) {
					continue
				}
				result.Processed++

				select {
				case <-ctx.Done():
					return
				case <-stop:
					return
				case repoC <- repo:
				}
			}
		}
	}()

	wg.Wait()

	if firstErr != nil {
		return Result{}, firstErr
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	return result, nil
}

// RunForRepository clones a single repository (owner/repo) and runs the processor on it.
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

// cloneDepth returns the depth to fetch, 0 meaning the whole history.
func cloneDepth(opts RunOptions) int {
	switch {
	case opts.CloneDepth == 0:
		return defaultCloneDepth
	case opts.CloneDepth < 0:
		return 0
	default:
		return opts.CloneDepth
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

	cloneDirName := repo.Name + "-" + cacheKey
	if len(opts.CloningSubset) > 0 {
		cloneDirName += "-" + hashCloningSubset(opts.CloningSubset)
	}

	cloneDir := path.Join(reposDir, cloneDirName)

	if err := ensureClone(ctx, repo, cloneDir, opts); err != nil {
		return "", err
	}

	if shouldReturnDirectly {
		return cloneDir, nil
	}

	xr := exec.NewExecerWithLogger(reposDir, logger)
	repoDir := cloneDir + "_" + randSequence(9)

	if _, err := xr.RunX(ctx, "cp", "-r", cloneDir, repoDir); err != nil {
		if rErr := os.RemoveAll(repoDir); rErr != nil {
			logger.Warn("Failed to remove the repo directory", "error", rErr)
		}
		return "", fmt.Errorf("copying repository %w", err)
	}

	return repoDir, nil
}

// cloneLocks holds a mutex per clone directory so concurrent workers sharing a clone cache key
// wait for the first clone to complete instead of copying a partial one.
var cloneLocks sync.Map

// ensureClone clones the repository to cloneDir unless it is already there.
func ensureClone(ctx context.Context, repo Repository, cloneDir string, opts RunOptions) error {
	m, _ := cloneLocks.LoadOrStore(cloneDir, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	cloneDirInfo, err := os.Stat(cloneDir)
	if err == nil {
		if !cloneDirInfo.IsDir() {
			return fmt.Errorf("unexpected file in cloning directory: %s", cloneDir)
		}

		return nil
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("checking clone directory: %w", err)
	}

	if err := os.MkdirAll(cloneDir, os.ModePerm); err != nil {
		return fmt.Errorf("creating cloning directory: %w", err)
	}

	if err := cloneRepository(ctx, repo, cloneDir, opts); err != nil {
		if rErr := os.RemoveAll(cloneDir); rErr != nil {
			log.FromCtx(ctx).Warn("Failed to remove the clone directory", "error", rErr)
		}

		return err
	}

	return nil
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

	fetchArgs := []string{"fetch"}
	if depth := cloneDepth(opts); depth > 0 {
		fetchArgs = append(fetchArgs, "--depth", strconv.Itoa(depth))
	}

	if _, err := xr.RunX(ctx, "git", append(fetchArgs, "origin", repo.DefaultBranchName)...); err != nil {
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
