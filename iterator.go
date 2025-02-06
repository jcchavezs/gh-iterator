package iterator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	execute "github.com/alexellis/go-execute/v2"
	"github.com/jcchavezs/gh-iterator/exec"
)

type Repository struct {
	Name             string `json:"nameWithOwner"`
	URL              string `json:"url"`
	SSHURL           string `json:"sshUrl"`
	DefaultBranchRef struct {
		Name string `json:"name"`
	} `json:"defaultBranchRef"`
}

func execCommand(ctx context.Context, printCommand bool, name string, arg ...string) (execute.ExecResult, error) {
	return execCommandWithDir(ctx, printCommand, "", name, arg...)
}

var errNonZeroExitCode = errors.New("non-zero exit code")

func execCommandWithDir(ctx context.Context, printCommand bool, dir, name string, arg ...string) (execute.ExecResult, error) {
	cmd := execute.ExecTask{
		Command:      name,
		Args:         arg,
		Cwd:          dir,
		PrintCommand: printCommand,
	}

	res, err := cmd.Execute(ctx)
	if (err != nil || res.ExitCode != 0) && printCommand {
		fmt.Println(res.Stderr)
	}

	if res.ExitCode != 0 && err == nil {
		err = errNonZeroExitCode
	}

	return res, err
}

var (
	baseDir  string
	reposDir string
)

func init() {
	baseDir, _ = filepath.Abs(os.TempDir())

	reposDir = path.Join(baseDir, "gh-iterator")
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		panic(err)
	}
}

type Processor func(ctx context.Context, repository string, exec exec.Execer) error

type Options struct {
	UseHTTPS      bool
	CloningSubset []string
	Debug         bool
}

const defaultLimit = "100"

func toGhArgs(f SearchOptions) []string {
	filters := []string{}
	if f.Language != "" {
		filters = append(filters, "--language", f.Language)
	}

	switch f.ArchiveCondition {
	case OnlyArchived:
		filters = append(filters, "--archived")
	case OmitArchived:
		filters = append(filters, "--no-archived")
	}

	switch f.Source {
	case OnlyForks:
		filters = append(filters, "--fork")
	case OnlyNonForks:
		filters = append(filters, "--source")
	}

	if f.Visibility != VisibilityNone {
		filters = append(filters, "--visibility", f.Visibility.String())
	}

	if f.Limit > 0 {
		filters = append(filters, "--limit", fmt.Sprintf("%d", f.Limit))
	} else {
		filters = append(filters, "--limit", defaultLimit)
	}

	return filters
}

func RunForOrganization(ctx context.Context, orgName string, filters SearchOptions, processor Processor, opts Options) error {
	args := append(
		[]string{"repo", "list", orgName, "--json", "nameWithOwner,defaultBranchRef,url,sshUrl"},
		toGhArgs(filters)...,
	)

	res, err := execCommand(ctx, opts.Debug, "gh", args...)
	if err != nil {
		return fmt.Errorf("listing repositories: %w", err)
	}

	repos := []Repository{}

	if err := json.Unmarshal([]byte(res.Stdout), &repos); err != nil {
		return fmt.Errorf("unmarshaling repositories: %w", err)
	}

	for _, repo := range repos {
		if err := processRepository(ctx, repo, processor, opts); err != nil {
			return err
		}
	}

	return nil
}

func RunForRepository(ctx context.Context, repoName string, processor Processor, opts Options) error {
	res, err := execCommand(ctx, opts.Debug, "gh", "repo", "view", repoName, "--json", "nameWithOwner,defaultBranchRef,url,sshUrl")
	if err != nil {
		return fmt.Errorf("fetching repository: %w", err)
	}

	repo := Repository{}

	err = json.Unmarshal([]byte(res.Stdout), &repo)
	if err != nil {
		return fmt.Errorf("unmarshaling repository: %w", err)
	}

	err = processRepository(ctx, repo, processor, opts)
	if err != nil {
		return err
	}

	return nil
}

func processRepository(ctx context.Context, repo Repository, processor Processor, opts Options) error {
	repoDir := path.Join(reposDir, repo.Name+time.Now().Format("-02150405"))

	if err := os.MkdirAll(repoDir, os.ModePerm); err != nil {
		return fmt.Errorf("creating cloning directory: %w", err)
	}
	defer os.RemoveAll(repoDir)

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "init"); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}

	repoURL := repo.SSHURL
	if opts.UseHTTPS {
		repoURL = repo.URL
	}

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "remote", "add", "origin", repoURL); err != nil {
		return fmt.Errorf("adding origin: %w", err)
	}

	if len(opts.CloningSubset) > 0 {
		if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "config", "core.sparseCheckout", "true"); err != nil {
			return fmt.Errorf("setting sparse checkout subset: %w", err)
		}

		if err := fillLines(filepath.Join(repoDir, ".git/info/sparse-checkout"), opts.CloningSubset); err != nil {
			return fmt.Errorf("setting cloning subset: %w", err)
		}
	}

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "fetch", "origin", repo.DefaultBranchRef.Name); err != nil {
		return fmt.Errorf("fetching HEAD: %w", err)
	}

	if _, err := execCommandWithDir(ctx, opts.Debug, repoDir, "git", "checkout", repo.DefaultBranchRef.Name); err != nil {
		return fmt.Errorf("checking out HEAD: %w", err)
	}

	if err := processor(ctx, repo.Name, exec.NewExecer(repoDir, opts.Debug)); err != nil {
		return fmt.Errorf("processing repository: %w", err)
	}

	return nil
}

func fillLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	for _, l := range lines {
		fmt.Fprintln(f, l)
	}

	return nil
}
