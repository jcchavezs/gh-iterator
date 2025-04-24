package exec

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestFS(t *testing.T) {
	dir := t.TempDir()
	e := NewExecer(dir, false)
	_, err := e.RunX(context.Background(), "touch", "a.txt")
	require.NoError(t, err)

	fs := e.FS()

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
