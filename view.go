package iterator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
	"github.com/jcchavezs/gh-iterator/internal/log"
)

type ViewRepositoriesOptions struct {
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers.
	NumberOfWorkers int

	// Log handler
	LogHandler slog.Handler
	// WrapExecer, if set, decorates every Execer the iterator creates, both the ones used
	// internally (gh) and the ones handed to the processor.
	WrapExecer func(exec.Execer) exec.Execer
	// OnRepository is called once per repository and returns the context used to process it plus
	// an optional function called with the final error once the repository is done.
	OnRepository func(ctx context.Context, repo Repository) (context.Context, func(err error))
}

func (o ViewRepositoriesOptions) runOptions() RunOptions {
	return RunOptions{
		NumberOfWorkers: o.NumberOfWorkers,
		LogHandler:      o.LogHandler,
		WrapExecer:      o.WrapExecer,
		OnRepository:    o.OnRepository,
	}
}

// ViewRepositoriesInOrganization lists the repositories for the given organization and processes them concurrently using the provided callback function.
// It returns a Result struct with the number of repositories found and inspected, or an error if any occurs during the process.
func ViewRepositoriesInOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts ViewRepositoriesOptions) (Result, error) {
	ctx, logger := setupLogger(ctx, opts.LogHandler, false)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages, err := streamRepoPages(ctx, searchOpts, orgName, logger)
	if err != nil {
		return Result{}, err
	}

	nOfWorkers := opts.NumberOfWorkers
	if nOfWorkers <= 0 {
		nOfWorkers = defaultNumberOfWorkers
	}

	filterIn := searchOpts.MakeFilterIn()
	xr := wrapExecer(opts.WrapExecer, newExecerWithLogger("", logger))

	return runForReposConcurrently(
		ctx,
		pages,
		nOfWorkers,
		filterIn,
		func(ctx context.Context, repo Repository, processor Processor, runOpts Options) error {
			logger := log.FromCtx(ctx).With("repository", repo.Name)
			processCtx, done := runOpts.beginRepository(log.NewCtx(ctx, logger), repo)

			err := processor(processCtx, repo, xr.WithEnv("GH_REPO", repo.Name))
			done(err)

			return err
		}, processor, opts.runOptions(),
	)
}

func ViewRepository(ctx context.Context, repoName string, processor Processor, opts ViewRepositoriesOptions) error {
	if strings.Count(repoName, "/") > 1 {
		return fmt.Errorf("incorrect repository name %q", repoName)
	}

	ctx, logger := setupLogger(ctx, opts.LogHandler, false, "repository", repoName)

	xr := wrapExecer(opts.WrapExecer, newExecerWithLogger(".", logger))

	ghArgs := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + GithubAPIVersion,
		"-X", "GET",
		"--jq", "{full_name,clone_url,ssh_url,default_branch,archived,language,visibility,fork,size,pushed_at,archived_at}",
		fmt.Sprintf("/repos/%s", repoName),
	}

	res, err := xr.RunX(ctx, "gh", ghArgs...)
	if err != nil {
		return fmt.Errorf("fetching repository %q: %w", repoName, github.ErrOrGHAPIErr(res, err))
	}

	repo := Repository{}
	err = json.Unmarshal([]byte(res), &repo)
	if err != nil {
		return fmt.Errorf("unmarshaling repository: %w", err)
	}

	ctx, done := opts.runOptions().beginRepository(ctx, repo)
	err = processor(ctx, repo, xr.WithEnv("GH_REPO", repo.Name))
	done(err)

	return err
}
