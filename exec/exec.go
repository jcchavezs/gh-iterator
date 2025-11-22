package exec

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexellis/go-execute/v2"
	"github.com/jcchavezs/gh-iterator/internal/log"
	"github.com/spf13/afero"
)

// Execer defines an interface to execute commands in a given directory
// with optional environment variables and logging capabilities.
type Execer interface {
	// Run executes a command with the repository's folder as working dir
	Run(ctx context.Context, command string, args ...string) (Result, error)
	// RunX executes a command with repository's folder as working dir. It will return an error
	// if exit code is non zero.
	RunX(ctx context.Context, command string, args ...string) (string, error)
	// Run executes a command with the repository's folder as working dir accepting a stdin
	RunWithStdin(ctx context.Context, stdin io.Reader, command string, args ...string) (Result, error)
	// RunWithStdin executes a command with the repository's folder as working dir accepting a stdin and returning the stdout
	RunWithStdinX(ctx context.Context, stdin io.Reader, command string, args ...string) (string, error)
	// Log logs a message with the given level and fields
	Log(ctx context.Context, level slog.Level, msg string, fields ...any)

	// DebugShell starts a shell session in the execer's directory
	DebugShell(ctx context.Context)

	// WithEnv creates a child execer with added env variables
	WithEnv(kv ...string) Execer
	// WithLogFields creates a child execer with added log fields
	WithLogFields(kvFields ...any) Execer
	// Sub creates a new execer in an existing subpath.
	Sub(subpath string) (Execer, error)
	// GenerateFS returns a FS object relative to the exec dir to interact with
	GenerateFS() afero.Fs
}

var _ Execer = execer{}

type execer struct {
	dir    string
	logger *slog.Logger
	env    []string
}

// NewExecer creates a new execer
func NewExecer(dir string) Execer {
	return execer{
		dir:    dir,
		logger: slog.New(log.DiscardHandler),
	}
}

// NewExecerWithLogger creates a new execer with a logger
func NewExecerWithLogger(dir string, logger *slog.Logger) Execer {
	return execer{
		dir:    dir,
		logger: logger,
	}
}

// WithEnv creates a child execer with added env variables
func (e execer) WithEnv(kv ...string) Execer {
	var env = e.env
	kvLen := len(kv)
	if kvLen <= 1 {
		return e
	} else if kvLen%2 != 0 {
		kv = kv[:kvLen-1]
	}

	for i := range kvLen / 2 {
		env = append(env, fmt.Sprintf("%s=%s", kv[2*i], kv[2*i+1]))
	}

	return execer{
		dir:    e.dir,
		logger: e.logger,
		env:    env,
	}
}

// WithLogFields creates a child execer with added log fields
func (e execer) WithLogFields(fields ...any) Execer {
	return execer{
		dir:    e.dir,
		logger: e.logger.With(fields...),
		env:    e.env,
	}
}

// Sub creates a new execer in an existing subpath.
func (e execer) Sub(subpath string) (Execer, error) {
	subdir := filepath.Join(e.dir, subpath)
	if finfo, err := os.Stat(subdir); err != nil {
		return execer{}, err
	} else if !finfo.IsDir() {
		return execer{}, fmt.Errorf("subpath %s is not a directory", subdir)
	}

	return execer{
		dir:    subdir,
		logger: e.logger,
		env:    e.env,
	}, nil
}

// GenerateFS returns a FS object relative to the exec dir to interact with
func (e execer) GenerateFS() afero.Fs {
	return afero.NewBasePathFs(afero.NewOsFs(), e.dir)
}

// Run executes a command with the repository's folder as working dir
func (e execer) Run(ctx context.Context, command string, args ...string) (Result, error) {
	return e.RunWithStdin(ctx, nil, command, args...)
}

// TrimStdout for convenience as RunX does not return a result where you can get the Result.TrimStdout
// but instead the stdout.
func TrimStdout(o string, err error) (string, error) {
	return strings.TrimSpace(o), err
}

// RunX executes a command with repository's folder as working dir. It will return an error
// if exit code is non zero.
func (e execer) RunX(ctx context.Context, command string, args ...string) (string, error) {
	res, err := e.Run(ctx, command, args...)
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return res.Stdout, NewExecErr(
			fmt.Sprintf("%s: exit code %d", cmdString(command, args...), res.ExitCode),
			res.Stderr, res.ExitCode,
		)
	}

	return res.Stdout, nil
}

// RunWithStdin executes a command with the repository's folder as working dir accepting a stdin
func (e execer) RunWithStdin(ctx context.Context, stdin io.Reader, command string, args ...string) (Result, error) {
	task := execute.ExecTask{
		Command: command,
		Args:    args,
		Cwd:     e.dir,
		Stdin:   stdin,
		Env:     e.env,
	}

	cmdS := cmdString(command, args...)
	e.logger.Debug("Executing command", "command", cmdS)

	execRes, err := task.Execute(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", cmdS, err)
	}

	return Result(execRes), nil
}

// RunWithStdin executes a command with the repository's folder as working dir accepting a stdin and returning the stdout
func (e execer) RunWithStdinX(ctx context.Context, stdin io.Reader, command string, args ...string) (string, error) {
	res, err := e.RunWithStdin(ctx, stdin, command, args...)
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return res.Stdout, NewExecErr(
			fmt.Sprintf("%s: exit code %d", cmdString(command, args...), res.ExitCode),
			res.Stderr, res.ExitCode,
		)
	}

	return res.Stdout, nil
}

// Log logs a message with the given level and fields
func (e execer) Log(ctx context.Context, level slog.Level, msg string, kvFields ...any) {
	e.logger.Log(ctx, level, msg, kvFields...)
}

func cmdString(command string, args ...string) string {
	return strings.Join(append([]string{command}, args...), " ")
}

// Result holds the result from a command run
type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Cancelled bool
}

// TrimStdout returns the content of stdout removing the trailing new lines.
func (r Result) TrimStdout() string {
	return strings.TrimSpace(r.Stdout)
}
