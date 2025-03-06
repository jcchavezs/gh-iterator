package exec

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alexellis/go-execute/v2"
)

type Execer struct {
	dir          string
	printCommand bool
}

func NewExecer(dir string, printCommand bool) Execer {
	return Execer{dir, printCommand}
}

// Run executes a command with the repository's folder as working dir
func (e Execer) Run(ctx context.Context, command string, args ...string) (Result, error) {
	return e.RunWithStdin(ctx, nil, command, args...)
}

// RunX executes a command with repository's folder as working dir. It will return an error
// if exit code is non zero.
func (e Execer) RunX(ctx context.Context, command string, args ...string) error {
	res, err := e.Run(ctx, command, args...)
	if err != nil {
		return err
	}

	if res.ExitCode() != 0 {
		return NewExecErr(
			fmt.Sprintf("%s: exit code %d", cmdString(command, args...), res.ExitCode()),
			res.Stderr(), res.ExitCode(),
		)
	}

	return nil
}

// Run executes a command with the repository's folder as working dir accepting a stdin
func (e Execer) RunWithStdin(ctx context.Context, stdin io.Reader, command string, args ...string) (Result, error) {
	task := execute.ExecTask{
		Command:      command,
		Args:         args,
		Cwd:          e.dir,
		PrintCommand: e.printCommand,
		Stdin:        stdin,
	}

	execRes, err := task.Execute(ctx)
	if err != nil {
		return result{}, fmt.Errorf("%s: %v", cmdString(command, args...), err)
	}

	return result{execRes}, nil
}

func cmdString(command string, args ...string) string {
	return strings.Join(append([]string{command}, args...), " ")
}

// Result holds the result from a command run
type Result interface {
	Stdout() string
	TrimStdout() string
	Stderr() string
	ExitCode() int
	Cancelled() bool
}

type result struct {
	execute.ExecResult
}

func (r result) Stdout() string {
	return r.ExecResult.Stdout
}

// TrimStdout returns the content of stdout removing the trailing new lines.
func (r result) TrimStdout() string {
	return strings.TrimSpace(r.ExecResult.Stdout)
}

func (r result) Stderr() string {
	return r.ExecResult.Stderr
}

func (r result) ExitCode() int {
	return r.ExecResult.ExitCode
}

func (r result) Cancelled() bool {
	return r.ExecResult.Cancelled
}
