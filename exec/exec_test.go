package exec

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestFS(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir)
	_, err := e.RunX(context.Background(), "touch", "a.txt")
	require.NoError(t, err)

	fs := e.GenerateFS()

	t.Run("exists", func(t *testing.T) {
		exists, err := afero.Exists(fs, "a.txt")
		require.True(t, exists)
		require.NoError(t, err)
	})

	t.Run("do not exist", func(t *testing.T) {
		exists, err := afero.Exists(fs, "b.txt")
		require.False(t, exists)
		require.NoError(t, err)
	})
}

func TestTrimStdout(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir)
	out, err := e.RunX(context.Background(), "echo", "Hello World")
	require.NoError(t, err)
	require.Equal(t, "Hello World\n", out)

	out, err = TrimStdout(e.RunX(context.Background(), "echo", "Hello World"))
	require.NoError(t, err)
	require.Equal(t, "Hello World", out)
}

func TestSub(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir)
	_, err := e.Sub("subdir")
	require.Error(t, err)

	_, err = e.RunX(context.Background(), "mkdir", "subdir")
	require.NoError(t, err)

	se, err := e.Sub("subdir")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "subdir"), se.(execer).dir)
}

func TestWithEnv(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir).(execer)

	t.Run("no env vars", func(t *testing.T) {
		childE := e.WithEnv().(execer)
		require.Equal(t, e.env, childE.env)
		require.Equal(t, e.dir, childE.dir)
	})

	t.Run("single env var pair", func(t *testing.T) {
		childE := e.WithEnv("KEY1", "value1").(execer)
		require.Equal(t, []string{"KEY1=value1"}, childE.env)
		require.Equal(t, e.dir, childE.dir)
	})

	t.Run("multiple env var pairs", func(t *testing.T) {
		childE := e.WithEnv("KEY1", "value1", "KEY2", "value2").(execer)
		require.Equal(t, []string{"KEY1=value1", "KEY2=value2"}, childE.env)
		require.Equal(t, e.dir, childE.dir)
	})

	t.Run("odd number of args", func(t *testing.T) {
		childE := e.WithEnv("KEY1", "value1", "KEY2").(execer)
		require.Equal(t, []string{"KEY1=value1"}, childE.env)
	})

	t.Run("with existing env vars", func(t *testing.T) {
		parentE := e.WithEnv("CHILD", "value1").(execer)
		childE := parentE.WithEnv("CHILD", "value2").(execer)
		require.Equal(t, []string{"CHILD=value1", "CHILD=value2"}, childE.env)

		res, err := childE.Run(context.Background(), os.Getenv("SHELL"), "-c", "echo \"$CHILD\"")
		require.NoError(t, err)
		require.Equal(t, "value2", res.TrimStdout())
	})
}

func TestExecerLog(t *testing.T) {
	dir := t.TempDir()

	t.Run("logs with different levels", func(t *testing.T) {
		var logOutput strings.Builder
		logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))

		e := NewExecerWithLogger(dir, logger)
		ctx := context.Background()

		e.Log(ctx, slog.LevelDebug, "debug message")
		e.Log(ctx, slog.LevelInfo, "info message")
		e.Log(ctx, slog.LevelWarn, "warn message")
		e.Log(ctx, slog.LevelError, "error message")

		output := logOutput.String()
		require.Contains(t, output, "info message")
		require.Contains(t, output, "warn message")
		require.Contains(t, output, "error message")
	})

	t.Run("logs with fields", func(t *testing.T) {
		var logOutput strings.Builder
		logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		e := NewExecerWithLogger(dir, logger)
		ctx := context.Background()

		e.Log(ctx, slog.LevelInfo, "message with fields", "key1", "value1", "key2", "value2")

		output := logOutput.String()
		require.Contains(t, output, "message with fields")
		require.Contains(t, output, "key1=value1")
		require.Contains(t, output, "key2=value2")
	})

	t.Run("logs with execer created with log fields", func(t *testing.T) {
		var logOutput strings.Builder
		logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		e := NewExecerWithLogger(dir, logger)
		eWithFields := e.WithLogFields("persistent", "field")
		ctx := context.Background()

		eWithFields.Log(ctx, slog.LevelInfo, "test message", "additional", "field")

		output := logOutput.String()
		require.Contains(t, output, "test message")
		require.Contains(t, output, "persistent=field")
		require.Contains(t, output, "additional=field")
	})
}

func TestOutputs(t *testing.T) {
	e := NewExecer(".")

	t.Run("successful execution", func(t *testing.T) {
		stdout, err := e.RunX(t.Context(), "go", "run", "./testdata/output/main.go")
		require.NoError(t, err)
		require.Equal(t, "stdout\n", stdout)
	})

	t.Run("failing execution", func(t *testing.T) {
		stdout, err := e.RunX(t.Context(), "go", "run", "./testdata/output/main.go", "fail")
		require.Error(t, err)
		require.Equal(t, "stdout\n", stdout)

		stderr, ok := GetStderr(err)
		require.True(t, ok)
		require.Equal(t, "stderr\nexit status 2\n", stderr)
	})
}

func TestStderrNotEmpty(t *testing.T) {
	t.Run("ok is false", func(t *testing.T) {
		result, ok := StderrNotEmpty("some stderr content", false)
		require.False(t, ok)
		require.Equal(t, "", result)
	})

	t.Run("stderr is empty string", func(t *testing.T) {
		result, ok := StderrNotEmpty("", true)
		require.False(t, ok)
		require.Equal(t, "", result)
	})

	t.Run("stderr is only whitespace", func(t *testing.T) {
		result, ok := StderrNotEmpty("   \n\t  ", true)
		require.False(t, ok)
		require.Equal(t, "", result)
	})

	t.Run("stderr has content", func(t *testing.T) {
		stderr := "error: something went wrong"
		result, ok := StderrNotEmpty(stderr, true)
		require.True(t, ok)
		require.Equal(t, stderr, result)
	})
}
