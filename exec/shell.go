package exec

import (
	"context"
	"fmt"
	"os"

	"github.com/alexellis/go-execute/v2"
	"github.com/jcchavezs/gh-iterator/internal/log"
	"golang.org/x/term"
)

// DebugShell starts a shell session in the execer's directory. It is useful to debug
// issues with the environment or the state of the directory.
func (e execer) DebugShell(ctx context.Context) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		log.FromCtx(ctx).Warn("Not a terminal, skipping shell debug")
		return
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		log.FromCtx(ctx).Warn("SHELL environment variable not set, cannot start debug shell")
		return
	}

	_, _ = fmt.Fprintln(os.Stdout, "Starting gh-iterator debug shell. Type 'exit' to resume.")

	task := execute.ExecTask{
		Command:      shell,
		Cwd:          e.dir,
		Stdin:        os.Stdin,
		StdErrWriter: os.Stderr,
		StdOutWriter: os.Stdout,
		Env:          append(e.env, "GH_ITERATOR_SHELL=true"),
	}

	_, err := task.Execute(ctx)
	if err != nil {
		log.FromCtx(ctx).Error("error starting debug shell", "error", err)
	}
}
