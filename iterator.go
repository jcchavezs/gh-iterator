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

	execute "github.com/alexellis/go-execute/v2"
	"github.com/jcchavezs/gh-iterator/exec"
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

func execCommand(ctx context.Context, printCommand bool, name string, arg ...string) (execute.ExecResult, error) {
	return execCommandWithDir(ctx, printCommand, "", name, arg...)
}

var errNonZeroExitCode = errors.New("non-zero exit code")

func execCommandWithDir(ctx context.Context, printCommand bool, dir, name string, arg ...string) (execute.ExecResult, error) {
	cmd := execute.ExecTask{
		Command:      name,
		Args:         arg,
		Cwd:          dir,
		PrintCommand: printCommand,
	}

	res, err := cmd.Execute(ctx)
	if (err != nil || res.ExitCode != 0) && printCommand {
		fmt.Println(res.Stderr)
	}

	if res.ExitCode != 0 && err == nil {
		fmt.Println(res.Stderr)
		err = errNonZeroExitCode
	}

	return res, err
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

// Processor is a function to process a repository.
// - ctx is the context to cancel the processing.
// - repository is the name of the repository.
// - isEmpty is a flag to indicate if the repository is empty i.e. no branches nor commits.
// - exec is an exec.Execer to run commands in the repository directory.
type Processor func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error

type Options struct {
	// UseHTTPS is a flag to use HTTPS instead of SSH to clone the repositories.
	UseHTTPS bool
	// CloningSubset is a list of files or directories to clone to avoid cloning the whole repository.
	// it is helpful on big repositories to speed up the process.
	CloningSubset []string
	// NumberOfWorkers is the number of workers to process the repositories concurrently.
	NumberOfWorkers int
	// Debug is a flag to print debug information.
	Debug bool
}

const (
	defaultNumberOfWorkers = 10
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
	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		"-X", "GET",
		"--jq", ". | map({full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size})",
	}

	target := fmt.Sprintf("/orgs/%s/repos", orgName)
	if searchOpts.PerPage == 0 || searchOpts.PerPage > maxPerPage {
		target = fmt.Sprintf("%s?per_page=%d", target, defaultPerPage)
	} else if searchOpts.PerPage > 0 {
		target = fmt.Sprintf("%s?per_page=%d", target, searchOpts.PerPage)
	} else {
		return Result{}, errors.New("invalid negative SearchOptions.PerPage")
	}

	if searchOpts.Page == -1 {
		ghArgs = append(ghArgs, "--paginate")
	} else if searchOpts.Page > 0 {
		target = fmt.Sprintf("%s&page=%d", target, searchOpts.Page)
	} else {
		return Result{}, errors.New("invalid negative SearchOptions.Page")
	}

	res, err := execCommand(ctx, opts.Debug, "gh", append(ghArgs, target)...)
	if err != nil {
		return Result{}, fmt.Errorf("fetching repositories: %w", err)
	}

	repoPages, err := processRepoPages(res.Stdout)
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

	for i := 0; i < nOfWorkers; i++ {
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
						if errors.Is(err, ErrNoDefaultBranch) {
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

	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		"-X", "GET",
		"--jq", "{full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size}",
		fmt.Sprintf("/repos/%s", repoName),
	}

	res, err := execCommand(ctx, opts.Debug, "gh", ghArgs...)
	if err != nil {
		return fmt.Errorf("fetching repositories: %w", err)
	}

	repo := Repository{}

	err = json.Unmarshal([]byte(res.Stdout), &repo)
	if err != nil {
		return fmt.Errorf("unmarshaling repository: %w", err)
	}

	err = processRepository(ctx, repo, processor, opts)
	if err != nil {
		return err
	}

	return nil
}

var (
	ErrNoDefaultBranch = errors.New("no default branch")
)

func processRepository(ctx context.Context, repo Repository, processor Processor, opts Options) error {
	if repo.Size == 0 {
		if err := processor(ctx, repo.Name, false, exec.NewExecer("", false)); err != nil {
			return fmt.Errorf("processing repository: %w", err)
		}
	}

	repoDir := path.Join(reposDir, repo.Name+time.Now().Format("-02150405"))

	if err := os.MkdirAll(repoDir, os.ModePerm); err != nil {
		return fmt.Errorf("creating cloning directory: %w", err)
	}
	defer os.RemoveAll(repoDir)

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "init"); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}

	repoURL := repo.SSHURL
	if opts.UseHTTPS {
		repoURL = repo.URL
	}

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "remote", "add", "origin", repoURL); err != nil {
		return fmt.Errorf("adding origin: %w", err)
	}

	if len(opts.CloningSubset) > 0 {
		if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "config", "core.sparseCheckout", "true"); err != nil {
			return fmt.Errorf("setting sparse checkout subset: %w", err)
		}

		if err := fillLines(filepath.Join(repoDir, ".git/info/sparse-checkout"), opts.CloningSubset); err != nil {
			return fmt.Errorf("setting cloning subset: %w", err)
		}
	}

	if repo.DefaultBranchName == "" {
		return ErrNoDefaultBranch
	}

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "fetch", "origin", repo.DefaultBranchName); err != nil {
		return fmt.Errorf("fetching HEAD: %w", err)
	}

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "checkout", repo.DefaultBranchName); err != nil {
		return fmt.Errorf("checking out HEAD: %w", err)
	}

	if err := processor(ctx, repo.Name, false, exec.NewExecer(repoDir, opts.Debug)); err != nil {
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
		fmt.Fprintln(f, l)
	}

	return nil
}
