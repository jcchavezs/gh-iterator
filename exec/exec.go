package exec

import (
	"context"

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
	execRes, err := execute.ExecTask{
		Command:      command,
		Args:         args,
		Cwd:          e.dir,
		PrintCommand: e.printCommand,
	}.Execute(ctx)

	return result{execRes}, err
}

type Result interface {
	Stdout() string
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

func (r result) Stderr() string {
	return r.ExecResult.Stderr
}

func (r result) ExitCode() int {
	return r.ExecResult.ExitCode
}

func (r result) Cancelled() bool {
	return r.ExecResult.Cancelled
}
