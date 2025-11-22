package github

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	for _, path := range paths {
		if _, err := exec.RunX(ctx, "git", "add", path); err != nil {
			// We return at first error because joining makes it cumbersome to get stderr from
			// as errors.Join have a different Unwrap signature
			return fmt.Errorf("adding files: %w", err)
		}
	}
	return nil
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

type PushOption bool

const (
	PushForce   PushOption = true
	PushNoForce PushOption = false
)

// Push updates remote refs along with associated objects
func Push(ctx context.Context, exec iteratorexec.Execer, branchName string, force PushOption) error {
	return PushToRemote(ctx, exec, "origin", branchName, force)
}

// PushToRemote updates remote refs along with associated objects
func PushToRemote(ctx context.Context, exec iteratorexec.Execer, remoteName string, branchName string, force PushOption) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	if branchName != "" {
		args = append(args, remoteName, branchName)
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
	// The branch that contains commits for your pull request
	Head string
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
			_, _ = f.WriteString(body)
			_ = f.Close()
			prBodyFile = f.Name()
			defer os.Remove(prBodyFile) //nolint:errcheck
		}
	}

	var (
		prURL   string
		isNewPR bool
		isDraft bool
	)

	// Passing the head ref to check if the PR exists. This is necessary when
	// we are checking PRs from forks.
	prViewArgs := []string{"pr", "view"}
	if opts.Head != "" {
		prViewArgs = append(prViewArgs, opts.Head)
	}
	prViewArgs = append(prViewArgs, "--json", "url,state,isDraft")

	if res, err := exec.Run(ctx, "gh", prViewArgs...); err != nil {
		return "", false, fmt.Errorf("checking existing PR: %w", err)
	} else if res.ExitCode == 0 {
		// PR exists
		var pr struct {
			URL     string `json:"url"`
			State   string `json:"state"`
			IsDraft bool   `json:"isDraft"`
		}

		if err := json.NewDecoder(strings.NewReader(res.Stdout)).Decode(&pr); err != nil {
			return "", false, fmt.Errorf("unmarshaling existing PR: %w", err)
		}

		isDraft = pr.IsDraft

		if pr.State != "CLOSED" && pr.State != "MERGED" {
			// PR is not closed
			prURL = pr.URL
		}
	}

	if prURL == "" {
		exec.Log(ctx, slog.LevelInfo, "Creating PR")
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
		if opts.Head != "" {
			createPRArgs = append(createPRArgs, "--head", opts.Head)
		}

		if prBodyFile == "" || opts.Title == "" {
			createPRArgs = append(createPRArgs, "--fill")
		}

		res, err := exec.RunX(ctx, "gh", createPRArgs...)
		if err != nil {
			return "", false, fmt.Errorf("failed to create PR: %w", ErrOrGHAPIErr(res, err))
		}

		prURL = strings.TrimSpace(res)
		isNewPR = true
	} else {
		exec.Log(ctx, slog.LevelDebug, "PR exists already exists", "url", prURL)

		createPRArgs := []string{"pr", "edit", prURL}
		if prBodyFile != "" {
			createPRArgs = append(createPRArgs, "--body-file", prBodyFile)
		}

		if opts.Title != "" {
			createPRArgs = append(createPRArgs, "--title", opts.Title)
		}

		if prBodyFile == "" || opts.Title == "" {
			createPRArgs = append(createPRArgs, "--fill")
		}

		res, err := exec.RunX(ctx, "gh", createPRArgs...)
		if err != nil {
			return "", false, fmt.Errorf("failed to update PR: %w", ErrOrGHAPIErr(res, err))
		}

		if isDraft != opts.Draft {
			toggleDraftArgs := []string{"pr", "ready", prURL}
			if isDraft {
				exec.Log(ctx, slog.LevelInfo, "Marking PR as ready for review")
			} else {
				exec.Log(ctx, slog.LevelInfo, "Marking PR as draft")
				toggleDraftArgs = append(toggleDraftArgs, "--undo")
			}

			if _, err := exec.RunX(ctx, "gh", toggleDraftArgs...); err != nil {
				return "", false, fmt.Errorf("failed to toggle draft status: %w", ErrOrGHAPIErr(res, err))
			}
		}
	}

	return prURL, isNewPR, nil
}

// ForkAndAddRemote a repository and add the remote to the local git config.
// It returns a function that given a branch name returns the head reference
// to be used in the PR creation (i.e., username:branchName).
// Important: if you name the remote as 'upstream', gh CLI might get confused when creating PRs.
func ForkAndAddRemote(ctx context.Context, exec iteratorexec.Execer, remoteName string) (func(branchName string) string, error) {
	username, err := getCurrentUser(ctx, exec)
	if err != nil {
		return nil, err
	}

	_, err = exec.RunX(ctx, "gh", "repo", "fork", "--remote", "--remote-name", remoteName)
	if err != nil {
		return nil, fmt.Errorf("forking repository and adding remote: %w", err)
	}

	return func(branchName string) string {
		return fmt.Sprintf("%s:%s", username, branchName)
	}, nil
}

func getCurrentUser(ctx context.Context, exec iteratorexec.Execer) (string, error) {
	res, err := iteratorexec.TrimStdout(exec.RunX(ctx, "gh", "api", "user", "--jq", ".login"))
	if err != nil {
		return "", fmt.Errorf("getting current user: %w", err)
	}

	return res, nil
}
