package mock

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestExecer_Run(t *testing.T) {
	expected := iteratorexec.Result{Stdout: "output", ExitCode: 0}
	x := Execer{
		RunFn: func(_ context.Context, command string, args ...string) (iteratorexec.Result, error) {
			require.True(t, CallIs(t, command, args, "git", "status"))
			return expected, nil
		},
	}

	result, err := x.Run(context.Background(), "git", "status")
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestExecer_RunX(t *testing.T) {
	x := Execer{
		RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
			require.True(t, CallIs(t, command, args, "git", "rev-parse", "HEAD"))
			return "abc123", nil
		},
	}

	out, err := x.RunX(context.Background(), "git", "rev-parse", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "abc123", out)
}

func TestExecer_RunWithStdin(t *testing.T) {
	expected := iteratorexec.Result{Stdout: "ok", ExitCode: 0}
	stdin := strings.NewReader("input")
	x := Execer{
		RunWithStdinFn: func(_ context.Context, r io.Reader, command string, args ...string) (iteratorexec.Result, error) {
			require.Equal(t, stdin, r)
			require.True(t, CallIs(t, command, args, "cat"))
			return expected, nil
		},
	}

	result, err := x.RunWithStdin(context.Background(), stdin, "cat")
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestExecer_RunWithStdinX(t *testing.T) {
	stdin := strings.NewReader("input")
	x := Execer{
		RunWithStdinXFn: func(_ context.Context, r io.Reader, command string, args ...string) (string, error) {
			require.Equal(t, stdin, r)
			require.True(t, CallIs(t, command, args, "cat"))
			return "input", nil
		},
	}

	out, err := x.RunWithStdinX(context.Background(), stdin, "cat")
	require.NoError(t, err)
	require.Equal(t, "input", out)
}

func TestExecer_Log_WithLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	x := Execer{Logger: logger}

	x.Log(context.Background(), slog.LevelInfo, "hello", "key", "val")

	require.Contains(t, buf.String(), "hello")
}

func TestExecer_Log_WithoutLogger(t *testing.T) {
	// Should not panic when Logger is nil
	x := Execer{}
	require.NotPanics(t, func() {
		x.Log(context.Background(), slog.LevelInfo, "hello")
	})
}

func TestExecer_DebugShell(t *testing.T) {
	x := Execer{}
	// Should be a no-op and not panic
	require.NotPanics(t, func() {
		x.DebugShell(context.Background())
	})
}

func TestExecer_WithEnv(t *testing.T) {
	// We need to return a new Execer from WithEnvFn to simulate the behavior of creating a new instance with the environment variables set.
	// We can't return x directly because that will fail the test as deep equality will not hold due to the function pointers even if they are copies
	child := Execer{}
	x := Execer{
		WithEnvFn: func(self Execer, kv ...string) iteratorexec.Execer {
			require.Equal(t, []string{"KEY", "VAL"}, kv)
			return child
		},
	}

	result := x.WithEnv("KEY", "VAL")
	require.Equal(t, child, result)
}

func TestExecer_WithLogFields(t *testing.T) {
	child := Execer{}
	x := Execer{
		WithLogFieldsFn: func(s Execer, fields ...any) iteratorexec.Execer {
			require.Equal(t, []any{"key", "val"}, fields)
			return child
		},
	}

	result := x.WithLogFields("key", "val")
	require.Equal(t, child, result)
}

func TestExecer_Sub(t *testing.T) {
	child := Execer{}
	x := Execer{
		SubFn: func(s Execer, subpath string) (iteratorexec.Execer, error) {
			require.Equal(t, "subdir", subpath)
			return child, nil
		},
	}

	result, err := x.Sub("subdir")
	require.NoError(t, err)
	require.Equal(t, child, result)
}

func TestExecer_GenerateFS(t *testing.T) {
	fs := afero.NewMemMapFs()
	x := Execer{
		GenerateFSFn: func() afero.Fs {
			return fs
		},
	}

	result := x.GenerateFS()
	require.Equal(t, fs, result)
}

func ExampleExecer() {
	xr := Execer{
		RunFn: func(_ context.Context, command string, args ...string) (iteratorexec.Result, error) {
			fmt.Println("Run called with:", command, args)
			return iteratorexec.Result{ExitCode: 0}, nil
		},
		WithEnvFn: func(self Execer, kv ...string) iteratorexec.Execer {
			fmt.Println("WithEnv called with:", kv)
			return self
		},
	}

	_, err := xr.WithEnv("USER", "tito").Run(context.Background(), "echo", "hello")
	if err != nil {
		fmt.Println("Error:", err)
	}

	// Output:
	// WithEnv called with: [USER tito]
	// Run called with: echo [hello]
}
