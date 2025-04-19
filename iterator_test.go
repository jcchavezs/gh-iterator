package iterator

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFillLines(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test")
	require.NoError(t, err)

	err = fillLines(f.Name(), []string{"README.md", "LICENSE"})
	require.NoError(t, err)

	require.NoError(t, f.Close())

	f, err = os.Open(f.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, f.Close())
	})

	fc, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "README.md\nLICENSE\n", string(fc))
}
