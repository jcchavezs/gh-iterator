package mock

import (
	"context"
	"io"
	"log/slog"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
	"github.com/spf13/afero"
)

// Execer is a mock implementation of exec.Execer
type Execer struct {
	RunFn           func(ctx context.Context, command string, args ...string) (iteratorexec.Result, error)
	RunXFn          func(ctx context.Context, command string, args ...string) (string, error)
	RunWithStdinFn  func(ctx context.Context, stdin io.Reader, command string, args ...string) (iteratorexec.Result, error)
	RunWithStdinXFn func(ctx context.Context, stdin io.Reader, command string, args ...string) (string, error)
	Logger          *slog.Logger

	// 'self' allows you to return the same mock Execer when calling WithEnv, WithLogFields or Sub
	// This is useful when you want to chain calls and keep the same mock behavior.
	WithEnvFn       func(self Execer, kv ...string) iteratorexec.Execer
	WithLogFieldsFn func(self Execer, fields ...any) iteratorexec.Execer
	SubFn           func(self Execer, subpath string) (iteratorexec.Execer, error)
	GenerateFSFn    func() afero.Fs
}

var _ iteratorexec.Execer = Execer{}

func (x Execer) Run(ctx context.Context, command string, args ...string) (iteratorexec.Result, error) {
	return x.RunFn(ctx, command, args...)
}

func (x Execer) RunX(ctx context.Context, command string, args ...string) (string, error) {
	return x.RunXFn(ctx, command, args...)
}

func (x Execer) RunWithStdin(ctx context.Context, stdin io.Reader, command string, args ...string) (iteratorexec.Result, error) {
	return x.RunWithStdinFn(ctx, stdin, command, args...)
}

func (x Execer) RunWithStdinX(ctx context.Context, stdin io.Reader, command string, args ...string) (string, error) {
	return x.RunWithStdinXFn(ctx, stdin, command, args...)
}

func (x Execer) Log(ctx context.Context, level slog.Level, msg string, fields ...any) {
	if x.Logger != nil {
		x.Logger.Log(ctx, level, msg, fields...)
	}
}

func (x Execer) DebugShell(context.Context) {}

func (x Execer) WithEnv(kv ...string) iteratorexec.Execer {
	return x.WithEnvFn(x, kv...)
}

func (x Execer) WithLogFields(fields ...any) iteratorexec.Execer {
	return x.WithLogFieldsFn(x, fields...)
}

func (x Execer) Sub(subpath string) (iteratorexec.Execer, error) {
	return x.SubFn(x, subpath)
}

func (x Execer) GenerateFS() afero.Fs {
	return x.GenerateFSFn()
}
