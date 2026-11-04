// Lists repositories of an organization that have a go.work file in the root.
// Usage: go run ./.programs/gowork-repos <org>
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/github"
)

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		_ = level.UnmarshalText([]byte(v))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gowork-repos <org>")
		os.Exit(2)
	}
	org := os.Args[1]
	logger := newLogger()
	ctx := context.Background()

	var (
		mu    sync.Mutex
		found []string
		done  atomic.Int64
	)
	start := time.Now()
	logger.Info("starting", "org", org, "file", "go.work")

	res, err := iterator.ViewRepositoriesInOrganization(ctx, org, iterator.SearchOptions{
		Languages:        []string{"Go"},
		ArchiveCondition: iterator.OmitArchived,
		SizeCondition:    iterator.NotEmpty,
		Page:             iterator.AllPages,
		Cache:            time.Hour,
	}, func(ctx context.Context, repo iterator.Repository, xr exec.Execer) error {
		n := done.Add(1)
		_, err := github.ReadFile(ctx, xr, repo.Name, "go.work")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				xr.Log(ctx, slog.LevelDebug, "no go.work", "repository", repo.Name, "n", n)
				return nil
			}
			return err
		}
		xr.Log(ctx, slog.LevelInfo, "found go.work", "repository", repo.Name, "n", n)
		mu.Lock()
		found = append(found, repo.Name)
		mu.Unlock()
		return nil
	}, iterator.ViewRepositoriesOptions{LogHandler: logger.Handler()})
	if err != nil {
		logger.Error("failed", "error", err)
		os.Exit(1)
	}

	sort.Strings(found)
	for _, name := range found {
		fmt.Println(name)
	}
	logger.Info("done", "matches", len(found), "found", res.Found, "inspected", res.Inspected, "processed", res.Processed, "elapsed", time.Since(start))
}
