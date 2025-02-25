package main

// This example runs check if README.md exists in all repositories for a given organization.
// To speed up the work it will just attempt to pull README.md from the remote repository.

import (
	"context"
	"fmt"
	"os"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
)

func main() {
	org := "jcchavezs"
	if len(os.Args) > 1 {
		org = os.Args[1]
	}

	const targetFile = "README.md"

	searchOpts := iterator.SearchOptions{Source: iterator.OnlyNonForks, PerPage: 10, ArchiveCondition: iterator.OmitArchived, SizeCondition: iterator.NotEmpty}

	_, err := iterator.RunForOrganization(
		context.Background(), org, searchOpts,
		func(ctx context.Context, repository string, _ bool, exec exec.Execer) error {
			res, err := exec.Run(ctx, "test", "-f", targetFile)
			if err != nil {
				return err
			}

			if res.ExitCode() == 0 {
				fmt.Printf("- Repository %s/%s has %s\n", org, repository, targetFile)
				return nil
			}

			fmt.Printf("- Repository %s/%s has no %s\n", org, repository, targetFile)

			return nil
		}, iterator.Options{CloningSubset: []string{targetFile}})
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
}
