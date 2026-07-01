package mock

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallIs_CommandMatch(t *testing.T) {
	require.True(t, CallIs(t, "git", []string{}, "git"))
}

func TestCallIs_CommandMismatch(t *testing.T) {
	require.False(t, CallIs(t, "git", []string{}, "gh"))
}

func TestCallIs_TooManyExpected(t *testing.T) {
	require.False(t, CallIs(t, "git", []string{"status"}, "git", "status", "extra"))
}

func TestCallIs_StringArgMatch(t *testing.T) {
	require.True(t, CallIs(t, "git", []string{"clone", "repo"}, "git", "clone", "repo"))
}

func TestCallIs_StringArgMismatch(t *testing.T) {
	require.False(t, CallIs(t, "git", []string{"clone", "repo"}, "git", "clone", "other"))
}

func TestCallIs_CallAnyArg(t *testing.T) {
	require.True(t, CallIs(t, "git", []string{"clone", "any-repo"}, "git", "clone", CallAny))
}

func TestCallIs_CallArgMatch(t *testing.T) {
	startsWithHTTPS := CallArg(func(s string) bool { return strings.HasPrefix(s, "https://") })
	require.True(t, CallIs(t, "git", []string{"clone", "https://github.com/org/repo"}, "git", "clone", startsWithHTTPS))
}

func TestCallIs_CallArgMismatch(t *testing.T) {
	startsWithHTTPS := CallArg(func(s string) bool { return strings.HasPrefix(s, "https://") })
	require.False(t, CallIs(t, "git", []string{"clone", "git@github.com:org/repo"}, "git", "clone", startsWithHTTPS))
}

func TestCallIs_FewerExpectedThanActualArgs(t *testing.T) {
	require.True(t, CallIs(t, "git", []string{"clone", "repo", "--depth=1"}, "git", "clone"))
}
