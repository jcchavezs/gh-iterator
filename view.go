package iterator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jcchavezs/gh-iterator/github"
	"github.com/jcchavezs/gh-iterator/internal/log"
)

// ViewRepositoriesOptions are the options to run the View* functions.
type ViewRepositoriesOptions struct {
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers.
	NumberOfWorkers int

	// Log handler
	LogHandler slog.Handler
}

// ViewRepositoriesForOwner runs the processor concurrently for the matching repositories of an
// organization or a user account without cloning them: the execer is not in a clone, use
// github.ReadFile or github.API to inspect them. The kind of owner is detected with the GitHub API,
// for users only the public repositories are listed. It is much faster than RunForOwner.
func ViewRepositoriesForOwner(ctx context.Context, owner string, searchOpts SearchOptions, processor Processor, opts ViewRepositoriesOptions) (Result, error) {
	ctx, logger := setupLogger(ctx, opts.LogHandler, false)

	ownerPath, err := resolveOwnerPath(ctx, logger, owner, searchOpts.Cache)
	if err != nil {
		return Result{}, err
	}

	return viewRepositories(ctx, logger, ownerPath, searchOpts, processor, opts)
}

// ViewRepositoriesInOrganization runs the processor concurrently for the matching repositories of an
// organization without cloning them: the execer is not in a clone, use github.ReadFile or github.API
// to inspect them. It is much faster than RunForOwner.
//
// Deprecated: use ViewRepositoriesForOwner, which also supports user accounts.
func ViewRepositoriesInOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, processor Processor, opts ViewRepositoriesOptions) (Result, error) {
	ctx, logger := setupLogger(ctx, opts.LogHandler, false)

	return viewRepositories(ctx, logger, orgsPath(orgName), searchOpts, processor, opts)
}

func viewRepositories(ctx context.Context, logger *slog.Logger, ownerPath string, searchOpts SearchOptions, processor Processor, opts ViewRepositoriesOptions) (Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages, err := streamPages(ctx, searchOpts, ownerPath, logger)
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
		pages,
		nOfWorkers,
		filterIn,
		func(ctx context.Context, repo Repository, processor Processor, opts Options) error {
			logger := log.FromCtx(ctx).With("repository", repo.Name)
			processCtx := log.NewCtx(ctx, logger)

			if err := processor(processCtx, repo, xr.WithEnv("GH_REPO", repo.Name)); err != nil {
				return err
			}

			return nil
		}, processor, RunOptions{
			NumberOfWorkers: opts.NumberOfWorkers,
			LogHandler:      opts.LogHandler,
		},
	)
}

// ViewRepository runs the processor for a single repository (owner/repo) without cloning it.
func ViewRepository(ctx context.Context, repoName string, processor Processor, opts ViewRepositoriesOptions) error {
	if strings.Count(repoName, "/") > 1 {
		return fmt.Errorf("incorrect repository name %q", repoName)
	}

	ctx, logger := setupLogger(ctx, opts.LogHandler, false, "repository", repoName)

	xr := newExecerWithLogger(".", logger)

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

	return processor(ctx, repo, xr.WithEnv("GH_REPO", repo.Name))
}
