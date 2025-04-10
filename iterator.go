package iterator

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
)

type Repository struct {
	Name              string `json:"full_name"`
	URL               string `json:"clone_url"`
	SSHURL            string `json:"ssh_url"`
	DefaultBranchName string `json:"default_branch"`
	Archived          bool   `json:"archived"`
	Language          string `json:"language"`
	Visibility        string `json:"visibility"`
	Fork              bool   `json:"fork"`
	Size              int    `json:"size"`
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
// - repository is the name of the repository.
// - isEmpty is a flag to indicate if the repository is empty i.e. no branches nor commits.
// - exec is an exec.Execer to run commands in the repository directory.
type Processor func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error

type CloneCacheKey func(repository Repository) string

func CloneCacheKeyFromString(s string) CloneCacheKey {
	return func(Repository) string { return s }
}

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
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers. Only valid when calling `RunForOrganization``
	NumberOfWorkers int
	// Debug is a flag to print debug information.
	Debug bool
	// ContextEnricher is a function to enrich the context before processing a repository.
	ContextEnricher func(context.Context, Repository) context.Context
}

const (
	defaultNumberOfWorkers = 10
	GithubAPIVersion       = "2022-11-28"
)

func processRepoPages(s string) ([][]Repository, error) {
	ls := bufio.NewScanner(strings.NewReader(s))
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

type Result struct {
	// Found is the total number of repositories found i.e. the total number of
	// repositories retrieved from the API.
	Found int
	// Inspected is the total number of repositories inspected before the filtering.
	Inspected int
	// Processed is the total number of repositories processed after the filtering.
	Processed int
}

// RunForOrganization runs the processor for all repositories in an organization.
func RunForOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts Options) (Result, error) {
	defer os.RemoveAll(reposDir)

	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + GithubAPIVersion,
		"-X", "GET",
		"--jq", ". | map({full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size})",
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
		return Result{}, errors.New("invalid negative SearchOptions.PerPage")
	}

	if searchOpts.Page == AllPages {
		ghArgs = append(ghArgs, "--paginate")
	} else if searchOpts.Page > 0 {
		target = fmt.Sprintf("%s&page=%d", target, searchOpts.Page)
	} else if searchOpts.Page != 0 {
		return Result{}, errors.New("invalid negative SearchOptions.Page")
	}

	exec := exec.NewExecer(".", opts.Debug)
	res, err := exec.RunX(ctx, "gh", append(ghArgs, target)...)
	if err != nil {
		return Result{}, fmt.Errorf("fetching repositories: %w", github.ErrOrGHAPIErr(res, err))
	}

	repoPages, err := processRepoPages(res)
	if err != nil {
		return Result{}, fmt.Errorf("processing repositories pages: %w", err)
	}

	var nOfWorkers = defaultNumberOfWorkers
	if opts.NumberOfWorkers > 0 {
		nOfWorkers = opts.NumberOfWorkers
	}

	var (
		repoC = make(chan Repository, nOfWorkers)
		errC  = make(chan error, nOfWorkers)
		doneC = make(chan struct{})
		wg    = sync.WaitGroup{}

		mFound, mInspected, mProcessed int
		mMux                           sync.Mutex
	)

	for _, repoPage := range repoPages {
		mFound += len(repoPage)
	}

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
					err = processRepository(ctx, repo, processor, opts)
					if err != nil {
						if errors.Is(err, errNoDefaultBranch) {
							fmt.Printf("WARN: repository %s has no default branch\n", repo.Name)
							continue
						}

						errC <- fmt.Errorf("processing %q: %w", repo.Name, err)
						return
					}
				}
			}
		}()
	}

	filterIn := searchOpts.MakeFilterIn()

	go func() {
		defer close(repoC)
		for _, repoPage := range repoPages {
			for _, repo := range repoPage {
				mMux.Lock()
				mInspected++
				if !filterIn(repo) {
					mMux.Unlock()
					continue
				}
				mProcessed++
				mMux.Unlock()

				select {
				case <-doneC:
					return
				case <-ctx.Done():
					return
				default:
					repoC <- repo
				}
			}
		}
		close(doneC)
	}()

	for {
		select {
		case err, ok := <-errC:
			if ok {
				close(doneC)
				wg.Wait()
				return Result{}, err
			}
		case <-ctx.Done():
			close(doneC)
			close(errC)
			return Result{}, ctx.Err()
		case <-doneC:
			wg.Wait()
			defer close(errC)

			select {
			case err := <-errC:
				return Result{}, err
			default:
				return Result{mFound, mInspected, mProcessed}, nil
			}
		}
	}
}

// RunForRepository runs the processor for a single repository.
func RunForRepository(ctx context.Context, repoName string, processor Processor, opts Options) error {
	if strings.Count(repoName, "/") > 1 {
		return fmt.Errorf("incorrect repository name %q", repoName)
	}

	exec := exec.NewExecer(".", opts.Debug)

	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + GithubAPIVersion,
		"-X", "GET",
		"--jq", "{full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size}",
		fmt.Sprintf("/repos/%s", repoName),
	}

	res, err := exec.RunX(ctx, "gh", ghArgs...)
	if err != nil {
		return fmt.Errorf("fetching repository %q: %w", repoName, github.ErrOrGHAPIErr(res, err))
	}

	repo := Repository{}
	err = json.Unmarshal([]byte(res), &repo)
	if err != nil {
		return fmt.Errorf("unmarshaling repository: %w", err)
	}

	if err = processRepository(ctx, repo, processor, opts); err != nil {
		return err
	}

	return nil
}

var (
	errNoDefaultBranch = errors.New("no default branch")
)

func cloneRepositoryOrGetFromCache(ctx context.Context, repo Repository, opts Options) (string, error) {
	var (
		cacheKey             string
		shouldReturnDirectly bool
	)

	if opts.CloneCacheKey != nil {
		cacheKey = opts.CloneCacheKey(repo)
	}

	if cacheKey == "" {
		shouldReturnDirectly = true
		cacheKey = time.Now().Format("02T15_04_05")
	}

	cloneDir := path.Join(reposDir, repo.Name+"-"+cacheKey)
	if cloneDirInfo, err := os.Stat(cloneDir); err == nil {
		if !cloneDirInfo.IsDir() {
			return "", fmt.Errorf("unexpected file in cloning directory: %s", cloneDir)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(cloneDir, os.ModePerm); err != nil {
			return "", fmt.Errorf("creating cloning directory: %w", err)
		}

		if err := cloneRepository(ctx, repo, cloneDir, opts); err != nil {
			os.RemoveAll(cloneDir)
			return "", err
		}
	} else {
		return "", fmt.Errorf("checking clone directory: %w", err)
	}

	if shouldReturnDirectly {
		return cloneDir, nil
	}

	exec := exec.NewExecer(reposDir, opts.Debug)
	repoDir := cloneDir + "_" + time.Now().Format("_999999")

	if _, err := exec.RunX(ctx, "rm", "-rf", repoDir); err != nil {
		return "", fmt.Errorf("removing repository %w", err)
	}
	if _, err := exec.RunX(ctx, "cp", "-r", cloneDir, repoDir); err != nil {
		os.RemoveAll(repoDir)
		return "", fmt.Errorf("copying repository %w", err)
	}

	return repoDir, nil
}

func cloneRepository(ctx context.Context, repo Repository, repoDir string, opts Options) error {
	exec := exec.NewExecer(repoDir, opts.Debug)

	if _, err := exec.RunX(ctx, "git", "init"); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}

	repoURL := repo.SSHURL
	if opts.UseHTTPS {
		repoURL = repo.URL
	}

	if _, err := exec.RunX(ctx, "git", "remote", "add", "origin", repoURL); err != nil {
		return fmt.Errorf("adding origin: %w", err)
	}

	if len(opts.CloningSubset) > 0 {
		if _, err := exec.RunX(ctx, "git", "config", "core.sparseCheckout", "true"); err != nil {
			return fmt.Errorf("setting sparse checkout subset: %w", err)
		}

		if err := fillLines(filepath.Join(repoDir, ".git/info/sparse-checkout"), opts.CloningSubset); err != nil {
			return fmt.Errorf("setting cloning subset: %w", err)
		}
	}

	if repo.DefaultBranchName == "" {
		return errNoDefaultBranch
	}

	if _, err := exec.RunX(ctx, "git", "fetch", "origin", repo.DefaultBranchName); err != nil {
		return fmt.Errorf("fetching HEAD: %w", err)
	}

	if _, err := exec.RunX(ctx, "git", "checkout", repo.DefaultBranchName); err != nil {
		return fmt.Errorf("checking out HEAD: %w", err)
	}

	return nil
}

func processRepository(ctx context.Context, repo Repository, processor Processor, opts Options) error {
	processCtx := ctx
	if opts.ContextEnricher != nil {
		processCtx = opts.ContextEnricher(ctx, repo)
	}

	if repo.Size == 0 {
		if err := processor(processCtx, repo.Name, true, exec.NewExecer("", false)); err != nil {
			return fmt.Errorf("processing empty repository: %w", err)
		}
	}

	repoDir, err := cloneRepositoryOrGetFromCache(ctx, repo, opts)
	if err != nil {
		return err
	}
	defer os.RemoveAll(repoDir)

	if err := processor(processCtx, repo.Name, false, exec.NewExecer(repoDir, opts.Debug)); err != nil {
		return fmt.Errorf("processing repository: %w", err)
	}

	return nil
}

// fillLines writes the lines to a file.
func fillLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	for _, l := range lines {
		if _, err := fmt.Fprintln(f, l); err != nil {
			return fmt.Errorf("printing lines to file: %w", err)
		}
	}

	return nil
}
