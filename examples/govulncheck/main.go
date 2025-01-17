package main

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
	defer f.Close()

	err = iterator.RunForOrganization(org, iterator.Filters{Language: "Go", Source: iterator.OnlyNonForks}, func(repository string, exec exec.Execer) error {
		fmt.Printf("Processing %s/%s\n", org, repository)

		res, err := exec.Run(context.Background(), "govulncheck", "./...")
		if err != nil {
			return fmt.Errorf("checking for vulnerabilities: %w", err)
		}

		if res.ExitCode() == 0 {
			fmt.Printf("No vulnerabilities found for %s/%s\n", org, repository)
		} else if len(res.Stdout()) > 0 {
			fmt.Fprintf(f, "%s\n%s\n", repository, strings.Repeat("-", len(repository)))
			f.WriteString(res.Stdout())
			f.WriteString("\n")
		}

		return nil
	}, iterator.Options{})
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
}
