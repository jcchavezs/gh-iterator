package github

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
)

func wrapErrIfNotNil(message string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf(message, err)
}

// Checks out a new branch
func CheckoutNewBranch(ctx context.Context, exec iteratorexec.Execer, name string) error {
	return wrapErrIfNotNil("creating branch: %w", exec.RunX(ctx, "git", "checkout", "-b", name))
}

// AddsFiles content to the index
func AddFiles(ctx context.Context, exec iteratorexec.Execer, paths ...string) error {
	var errs = []error{}
	for _, path := range paths {
		if err := exec.RunX(ctx, "git", "add", path); err != nil {
			errs = append(errs, err)
		}
	}
	return wrapErrIfNotNil("adding files: %w", errors.Join(errs...))
}

// HasChanges returns true if files are changed in the working tree status
func HasChanges(ctx context.Context, exec iteratorexec.Execer) (bool, error) {
	res, err := exec.Run(ctx, "git", "status", "-s")
	if err != nil {
		return false, fmt.Errorf("checking changes: %w", err)
	}

	return len(res.TrimStdout()) > 0, nil
}

// ListChanges return a lis of changes in the working tree status
func ListChanges(ctx context.Context, exec iteratorexec.Execer) ([][2]string, error) {
	res, err := exec.Run(ctx, "git", "status", "-s")
	if err != nil {
		return nil, fmt.Errorf("listing changes: %w", err)
	}

	changes := [][2]string{}

	scanner := bufio.NewScanner(strings.NewReader(res.TrimStdout()))
	for scanner.Scan() {
		if l := scanner.Text(); len(l) > 0 {
			t, file, _ := strings.Cut(l, " ")
			changes = append(changes, [2]string{t, file})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("listing changes: %w", err)
	}

	return changes, nil
}

// Commit records changes to the repository
func Commit(ctx context.Context, exec iteratorexec.Execer, message string, flags ...string) error {
	args := append([]string{"commit", "-m", message}, flags...)
	return wrapErrIfNotNil("commiting changes: %w", exec.RunX(ctx, "git", args...))
}

// Push updates remote refs along with associated objects
func Push(ctx context.Context, exec iteratorexec.Execer, branchName string, force bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	if branchName != "" {
		args = append(args, "origin", branchName)
	}

	return wrapErrIfNotNil("pushing changes: %w", exec.RunX(ctx, "git", args...))
}

type PROptions struct {
	Title  string
	Body   string
	DryRun bool
	Draft  bool
}

type pr struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

const prBodyMaxLen = 5000 // arbitrary but I think it is enough

// CreatePRIfNotExist on GitHub
func CreatePRIfNotExist(ctx context.Context, exec iteratorexec.Execer, opts PROptions) (string, error) {
	var prBodyFile string

	if len(opts.Body) > 0 {
		body := opts.Body
		if len(body) > prBodyMaxLen {
			body = body[:prBodyMaxLen]
		}

		if f, err := os.CreateTemp(os.TempDir(), "pr-body"); err != nil {
			return "", fmt.Errorf("creating PR body file: %w", err)
		} else {
			f.WriteString(body)
			f.Close()
			prBodyFile = f.Name()
			defer os.Remove(prBodyFile)
		}
	}

	var prURL string
	if res, err := exec.Run(ctx, "gh", "pr", "view", "--json", "url,state"); err != nil {
		return "", fmt.Errorf("checking existing PR: %w", err)
	} else if res.ExitCode() == 0 {
		pr := &pr{}
		if err := json.NewDecoder(strings.NewReader(res.Stdout())).Decode(pr); err != nil {
			return "", fmt.Errorf("unmarshaling existing PR: %w", err)
		}

		if pr.State != "CLOSED" {
			prURL = pr.URL
		}
	}

	if prURL == "" {
		createPRArgs := []string{"pr", "create"}
		if prBodyFile != "" {
			createPRArgs = append(createPRArgs, "--body-file", prBodyFile)
		}
		if opts.Draft {
			createPRArgs = append(createPRArgs, "--draft")
		}
		if opts.Title != "" {
			createPRArgs = append(createPRArgs, "--title", opts.Title)
		}

		if opts.DryRun {
			createPRArgs = append(createPRArgs, "--dry-run")
		}

		res, err := exec.Run(ctx, "gh", createPRArgs...)
		if err != nil {
			return "", fmt.Errorf("failed to create PR: %w", err)
		}

		if res.ExitCode() != 0 {
			return "", iteratorexec.NewExecErr("failed to create PR", res.Stderr(), res.ExitCode())
		}

		prURL = res.TrimStdout()
	}

	return prURL, nil
}
