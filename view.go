package iterator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
	"github.com/jcchavezs/gh-iterator/internal/log"
)

type ViewRepositoriesOptions struct {
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers.
	NumberOfWorkers int

	// Log handler
	LogHandler slog.Handler
}

// ViewRepositoriesInOrganization lists the repositories for the given organization and processes them concurrently using the provided callback function.
// It returns a Result struct with the number of repositories found and inspected, or an error if any occurs during the process.
func ViewRepositoriesInOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, callback func(ctx context.Context, xr iteratorexec.Execer, repository Repository) error, opts ViewRepositoriesOptions) (Result, error) {
	ctx, logger := setupLogger(ctx, opts.LogHandler, false)

	repoPages, err := getRepoPages(ctx, searchOpts, orgName, logger)
	if err != nil {
		return Result{}, err
	}

	nOfWorkers := opts.NumberOfWorkers
	if nOfWorkers <= 0 {
		nOfWorkers = defaultNumberOfWorkers
	}

	filterIn := searchOpts.MakeFilterIn()
	xr := newExecerWithLogger("", logger)

	return runForReposConcurrently(
		ctx,
		repoPages,
		nOfWorkers,
		filterIn,
		func(ctx context.Context, repo Repository, processor Processor, opts Options) error {
			logger := log.FromCtx(ctx).With("repository", repo.Name)
			processCtx := log.NewCtx(ctx, logger)

			if err := processor(processCtx, repo, xr.WithEnv("GH_REPO", repo.Name)); err != nil {
				return err
			}

			return nil
		},
		func(ctx context.Context, repo Repository, xr iteratorexec.Execer) error {
			return callback(ctx, xr, repo)
		}, RunOptions{
			NumberOfWorkers: opts.NumberOfWorkers,
			LogHandler:      opts.LogHandler,
		},
	)
}

func ViewRepository(ctx context.Context, repoName string, callback func(ctx context.Context, xr iteratorexec.Execer, repository Repository) error, opts ViewRepositoriesOptions) error {
	if strings.Count(repoName, "/") > 1 {
		return fmt.Errorf("incorrect repository name %q", repoName)
	}

	ctx, logger := setupLogger(ctx, opts.LogHandler, false)

	x := newExecerWithLogger(".", logger)

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

	if err = processRepository(ctx, repo, func(ctx context.Context, repository Repository, xr iteratorexec.Execer) error {
		return callback(ctx, xr, repository)
	}, RunOptions{
		NumberOfWorkers: opts.NumberOfWorkers,
		LogHandler:      opts.LogHandler,
	}); err != nil {
		return fmt.Errorf("processing %q: %w", repo.Name, err)
	}

	return nil
}
