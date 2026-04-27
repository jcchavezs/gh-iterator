package iterator

import (
	"context"
	"log/slog"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/internal/log"
)

type ListOptions struct {
	// NumberOfWorkers is the number of workers to process the repositories concurrently, by default it
	// uses 10 workers. Only valid when calling `RunForOrganization``
	NumberOfWorkers int

	// Log handler
	LogHandler slog.Handler
}

// ListForOrganization lists the repositories for the given organization and processes them concurrently using the provided callback function.
// It returns a Result struct with the number of repositories found and inspected, or an error if any occurs during the process.
func ListForOrganization(ctx context.Context, orgName string, searchOpts SearchOptions, callback func(context.Context, iteratorexec.Execer, string) error, opts ListOptions) (Result, error) {
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
	xr := iteratorexec.NewExecerWithLogger("", logger)

	return runForReposConcurrently(
		ctx,
		repoPages,
		nOfWorkers,
		filterIn,
		func(ctx context.Context, repo Repository, processor Processor, opts Options) error {
			logger := log.FromCtx(ctx).With("repository", repo.Name)
			processCtx := log.NewCtx(ctx, logger)

			if err := processor(processCtx, repo.Name, repo.Size == 0, xr); err != nil {
				return err
			}

			return nil
		},
		func(ctx context.Context, repository string, isEmpty bool, xr iteratorexec.Execer) error {
			return callback(ctx, xr, repository)
		}, RunOptions{
			NumberOfWorkers: opts.NumberOfWorkers,
			LogHandler:      opts.LogHandler,
		},
	)
}
