package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
)

func CheckoutBranch(ctx context.Context, exec iteratorexec.Execer, name string) error {
	res, err := exec.Run(ctx, "git", "checkout", "-b", name)
	if err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	if res.ExitCode() != 0 {
		return iteratorexec.NewExecErr("creating branch", res.Stderr(), res.ExitCode())
	}

	return nil
}

func Add(ctx context.Context, exec iteratorexec.Execer, paths ...string) error {
	var errs = make([]error, 0, len(paths))
	for _, path := range paths {
		res, err := exec.Run(ctx, "git", "add", path)
		if err != nil {
			errs = append(errs, err)
		}

		if res.ExitCode() != 0 {
			errs = append(errs, iteratorexec.NewExecErr("adding path", res.Stderr(), res.ExitCode()))
		}
	}
	return errors.Join(errs...)
}

func Commit(ctx context.Context, exec iteratorexec.Execer, message string, flags ...string) error {
	args := append([]string{"commit", "-m", message}, flags...)
	res, err := exec.Run(ctx, "git", args...)
	if err != nil {
		return fmt.Errorf("commiting changes: %w", err)
	}

	if res.ExitCode() != 0 {
		return iteratorexec.NewExecErr("commiting changes", res.Stderr(), res.ExitCode())
	}

	return err
}

func Push(ctx context.Context, exec iteratorexec.Execer, branchName string, force bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	if branchName != "" {
		args = append(args, "origin", branchName)
	}

	res, err := exec.Run(ctx, "git", args...)
	if err != nil {
		return fmt.Errorf("pushing changes: %w", err)
	}

	if res.ExitCode() != 0 {
		return iteratorexec.NewExecErr("pushing changes", res.Stderr(), res.ExitCode())
	}

	return nil
}

type PROptions struct {
	Title  string
	Body   string
	DryRun bool
}

type pr struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

func CreatePRIfNotExist(ctx context.Context, exec iteratorexec.Execer, opts PROptions) (string, error) {
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
		createPRArgs := []string{"pr", "create", "--body-file", opts.Body, "--draft", "--title", opts.Title}
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
