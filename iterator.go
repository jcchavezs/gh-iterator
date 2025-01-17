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
	Name             string `json:"name"`
	URL              string `json:"url"`
	DefaultBranchRef struct {
		Name string `json:"name"`
	} `json:"defaultBranchRef"`
}

func execCommand(printCommand bool, name string, arg ...string) (execute.ExecResult, error) {
	return execCommandWithDir(printCommand, "", name, arg...)
}

var errNonZeroExitCode = errors.New("non-zero exit code")

func execCommandWithDir(printCommand bool, dir, name string, arg ...string) (execute.ExecResult, error) {
	cmd := execute.ExecTask{
		Command:      name,
		Args:         arg,
		Cwd:          dir,
		PrintCommand: printCommand,
	}

	res, err := cmd.Execute(context.Background())
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

type Processor func(repository string, exec exec.Execer) error

type Options struct {
	Debug bool
}

func toGhArgs(f Filters) []string {
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

	return filters
}

func RunForOrganization(orgName string, filters Filters, processor Processor, opts Options) error {
	args := append(
		[]string{"repo", "list", orgName, "--json", "name,defaultBranchRef,url", "--limit", "500"},
		toGhArgs(filters)...,
	)

	res, err := execCommand(opts.Debug, "gh", args...)
	if err != nil {
		return fmt.Errorf("listing repositories: %w", err)
	}

	repos := []Repository{}

	if err := json.Unmarshal([]byte(res.Stdout), &repos); err != nil {
		return fmt.Errorf("unmarshaling repositories: %w", err)
	}

	for _, repo := range repos {
		if err := processRepository(repo, processor, opts); err != nil {
			return fmt.Errorf("processing repository: %w", err)
		}
	}

	return nil
}

func RunForRepository(repoName string, processor Processor, opts Options) error {
	res, err := execCommand(opts.Debug, "gh", "repo", "view", repoName, "--json", "name,defaultBranchRef,url")
	if err != nil {
		return fmt.Errorf("fetching repository: %w", err)
	}

	repo := Repository{}

	err = json.Unmarshal([]byte(res.Stdout), &repo)
	if err != nil {
		return fmt.Errorf("unmarshaling repository: %w", err)
	}

	err = processRepository(repo, processor, opts)
	if err != nil {
		return fmt.Errorf("patching repository: %w", err)
	}

	return nil
}

func processRepository(repo Repository, processor Processor, opts Options) error {
	repoDir := path.Join(reposDir, repo.Name+time.Now().Format("-02150405"))

	if _, err := execCommand(opts.Debug, "gh", "repo", "clone", repo.URL, repoDir); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}
	defer os.RemoveAll(repoDir)

	if err := processor(repo.Name, exec.NewExecer(repoDir, opts.Debug)); err != nil {
		return fmt.Errorf("processing repository: %w", err)
	}

	return nil
}
