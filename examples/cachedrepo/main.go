package main

// This example runs clones the repository 100 times, if CACHE_KEY env var is passed with a non
// empty value it will clone the repository once to a cache and reuse copies of it. If the same value
// is passed again to a new execution it will reuse the cache.

import (
	"context"
	"fmt"
	"os"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
)

func main() {
	repo := "jcchavezs/gh-iterator"
	if len(os.Args) > 1 {
		repo = os.Args[1]
	}

	const readmeFile = "README.md"
	var rErr error

	for range 100 {
		if err := iterator.RunForRepository(
			context.Background(), repo,
			func(ctx context.Context, repository string, _ bool, exec exec.Execer) error {
				fmt.Println("Hello")

				return nil
			}, iterator.Options{
				CloneCacheKey: func(iterator.Repository) string {
					return os.Getenv("CACHE_KEY")
				},
				CloningSubset: []string{readmeFile}, // Only clone README.md
			}); err != nil {
			rErr = err
			break
		}
	}

	if rErr != nil {
		fmt.Printf("ERROR: %v\n", rErr)
		os.Exit(1)
	}
}
