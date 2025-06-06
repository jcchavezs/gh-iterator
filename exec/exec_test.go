package exec

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestFS(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir, false)
	_, err := e.RunX(context.Background(), "touch", "a.txt")
	require.NoError(t, err)

	fs := FS(e)

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
	e := NewExecer(dir, false)
	out, err := e.RunX(context.Background(), "echo", "Hello World")
	require.NoError(t, err)
	require.Equal(t, "Hello World\n", out)

	out, err = TrimStdout(e.RunX(context.Background(), "echo", "Hello World"))
	require.NoError(t, err)
	require.Equal(t, "Hello World", out)
}

func TestSub(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir, false)
	_, err := Sub(e, "subdir")
	require.Error(t, err)

	_, err = e.RunX(context.Background(), "mkdir", "subdir")
	require.NoError(t, err)

	se, err := Sub(e, "subdir")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "subdir"), se.dir)
}
