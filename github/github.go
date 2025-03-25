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

type ghErrResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

func (r ghErrResponse) Error() string {
	return fmt.Sprintf("%s with status %s", strings.ToLower(r.Message), r.Status)
}

// ErrOrGHAPIErr unmarshals the response payload and if it success return the GH API error,
// otherwise returns the generic error.
func ErrOrGHAPIErr(apiResponsePayload string, err error) error {
	if len(apiResponsePayload) > 0 {
		var errRes ghErrResponse
		if dErr := json.NewDecoder(strings.NewReader(apiResponsePayload)).Decode(&errRes); dErr == nil {
			return errRes
		}
	}

	return err
}

func wrapErrIfNotNil(message string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf(message, err)
}

// CurrentBranch returns the current branch
func CurrentBranch(ctx context.Context, exec iteratorexec.Execer) (string, error) {
	res, err := exec.RunX(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(res), wrapErrIfNotNil("creating branch: %w", err)
}

// Checks out a new branch
func CheckoutNewBranch(ctx context.Context, exec iteratorexec.Execer, name string) error {
	_, err := exec.RunX(ctx, "git", "checkout", "-b", name)
	return wrapErrIfNotNil("creating branch: %w", err)
}

// AddsFiles content to the index
func AddFiles(ctx context.Context, exec iteratorexec.Execer, paths ...string) error {
	var errs = []error{}
	for _, path := range paths {
		if _, err := exec.RunX(ctx, "git", "add", path); err != nil {
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

// ListChanges return a list of changes in the working tree status
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
	_, err := exec.RunX(ctx, "git", args...)
	return wrapErrIfNotNil("commiting changes: %w", err)
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

	_, err := exec.RunX(ctx, "git", args...)
	return wrapErrIfNotNil("pushing changes: %w", err)
}

type PROptions struct {
	// Title for the pull request
	Title string
	// Body for the pull request
	Body string
	// Draft will open the PR as draft when true
	Draft bool
}

type pr struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

const prBodyMaxLen = 5000 // arbitrary but I think it is enough

// CreatePRIfNotExist on GitHub and returns:
// - The PR URL
// - Whether the PR is new or not
// - An error if occurred.
func CreatePRIfNotExist(ctx context.Context, exec iteratorexec.Execer, opts PROptions) (string, bool, error) {
	var prBodyFile string

	if len(opts.Body) > 0 {
		body := opts.Body
		if len(body) > prBodyMaxLen {
			body = body[:prBodyMaxLen]
		}

		if f, err := os.CreateTemp(os.TempDir(), "pr-body"); err != nil {
			return "", false, fmt.Errorf("creating PR body file: %w", err)
		} else {
			f.WriteString(body)
			f.Close()
			prBodyFile = f.Name()
			defer os.Remove(prBodyFile)
		}
	}

	var (
		prURL   string
		isNewPR bool
	)
	if res, err := exec.Run(ctx, "gh", "pr", "view", "--json", "url,state"); err != nil {
		return "", false, fmt.Errorf("checking existing PR: %w", err)
	} else if res.ExitCode() == 0 {
		// PR exists
		pr := &pr{}
		if err := json.NewDecoder(strings.NewReader(res.Stdout())).Decode(pr); err != nil {
			return "", false, fmt.Errorf("unmarshaling existing PR: %w", err)
		}

		if pr.State != "CLOSED" {
			// PR is not closed
			prURL = pr.URL
		}
	}

	if prURL == "" {
		// non Closed PR does not exist
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

		res, err := exec.RunX(ctx, "gh", createPRArgs...)
		if err != nil {
			return "", false, fmt.Errorf("failed to create PR: %w", ErrOrGHAPIErr(res, err))
		}

		prURL = res
		isNewPR = true
	} else {
		createPRArgs := []string{"pr", "edit"}
		if prBodyFile != "" {
			createPRArgs = append(createPRArgs, "--body-file", prBodyFile)
		}

		if opts.Title != "" {
			createPRArgs = append(createPRArgs, "--title", opts.Title)
		}

		res, err := exec.RunX(ctx, "gh", createPRArgs...)
		if err != nil {
			return "", false, fmt.Errorf("failed to update PR: %w", ErrOrGHAPIErr(res, err))
		}
	}

	return prURL, isNewPR, nil
}
