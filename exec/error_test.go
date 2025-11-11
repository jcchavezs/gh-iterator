package exec

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetStderr(t *testing.T) {
	t.Run("returns stderr from exec error", func(t *testing.T) {
		err := NewExecErr("command failed", "error output", 1)
		stderr, ok := GetStderr(err)
		require.True(t, ok)
		require.Equal(t, "error output", stderr)
	})

	t.Run("returns false for non-exec error", func(t *testing.T) {
		err := errors.New("some other error")
		stderr, ok := GetStderr(err)
		require.False(t, ok)
		require.Empty(t, stderr)
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		stderr, ok := GetStderr(nil)
		require.False(t, ok)
		require.Empty(t, stderr)
	})

	t.Run("returns stderr from wrapped exec error", func(t *testing.T) {
		execErr := NewExecErr("command failed", "wrapped error output", 2)
		wrappedErr := errors.Join(errors.New("outer error"), execErr)
		stderr, ok := GetStderr(wrappedErr)
		require.True(t, ok)
		require.Equal(t, "wrapped error output", stderr)

		wrappedErr = fmt.Errorf("additional context: %w", execErr)
		stderr, ok = GetStderr(wrappedErr)
		require.True(t, ok)
		require.Equal(t, "wrapped error output", stderr)
	})
}
