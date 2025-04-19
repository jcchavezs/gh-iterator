package main

// This example runs govulncheck in all Go repositories for a given organization

import (
	"context"
	"fmt"
	"os"
	"strings"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
)

func main() {
	org := "jcchavezs"
	if len(os.Args) > 1 {
		org = os.Args[1]
	}

	f, err := os.Create(fmt.Sprintf("./govulncheck-%s-report.txt", org))
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
	}
	defer f.Close() //nolint:errcheck

	_, err = iterator.RunForOrganization(context.Background(), org, iterator.SearchOptions{Languages: []string{"Go"}, Source: iterator.OnlyNonForks, PerPage: 20, SizeCondition: iterator.NotEmpty}, func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
		fmt.Printf("Processing %s/%s\n", org, repository)

		res, err := exec.Run(ctx, "govulncheck", "./...")
		if err != nil {
			return fmt.Errorf("checking for vulnerabilities: %w", err)
		}

		if res.ExitCode() == 0 {
			_, _ = fmt.Printf("No vulnerabilities found for %s/%s\n", org, repository)
		} else if len(res.TrimStdout()) > 0 {
			_, _ = fmt.Fprintf(f, "%s\n%s\n", repository, strings.Repeat("-", len(repository)))
			_, _ = f.WriteString(res.Stdout())
			_, _ = f.WriteString("\n")
		}

		return nil
	}, iterator.Options{Debug: true})
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
}
