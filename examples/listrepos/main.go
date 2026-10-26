package main

// This example lists all repositories for a given organization and checks
// if they have a dependabot manifest.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	iterator "github.com/jcchavezs/gh-iterator"
	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
)

func main() {
	var (
		boardMux sync.Mutex
		onboard  = 0
		offboard = 0
	)

	res, err := iterator.ListForOrganization(
		context.Background(),
		"jcchavezs",
		iterator.SearchOptions{
			Page: iterator.AllPages,
		},
		func(ctx context.Context, xr iteratorexec.Execer, repo string) error {
			path := ".github/dependabot.yml"

			_, err := github.ReadFile(ctx, xr, repo, path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				xr.Log(ctx, slog.LevelError, "Failed to read dependabot manifest", "repository", repo, "error", err)
				return nil
			}

			boardMux.Lock()
			defer boardMux.Unlock()

			if err == nil {
				onboard++
			} else {
				offboard++
			}

			return nil
		}, iterator.ListOptions{
			NumberOfWorkers: 5,
		},
	)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Total repositories: %d\n", res.Processed)
	fmt.Printf("Onboarded repositories: %d\n", onboard)
	fmt.Printf("Offboarded repositories: %d\n", offboard)
}
