package require

import (
	"github.com/stretchr/testify/require"
)

type tHelper = interface {
	Helper()
}

// ArgEqual asserts that the argument at position i in args is equal to expected.
func ArgEqual(t require.TestingT, expected any, args []string, i int, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	require.Greater(t, len(args), i, "not enough arguments to compare")
	require.Equal(t, expected, args[i], msgAndArgs...)
}
