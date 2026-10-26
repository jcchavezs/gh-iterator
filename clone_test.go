package iterator

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/mock"
	"github.com/stretchr/testify/require"
)

func TestFetchArgs(t *testing.T) {
	tests := map[string]struct {
		depth    int
		expected []string
	}{
		"default is shallow": {0, []string{"fetch", "--depth=1", "origin", "main"}},
		"explicit depth":     {5, []string{"fetch", "--depth=5", "origin", "main"}},
		"negative is full":   {-1, []string{"fetch", "origin", "main"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, fetchArgs(tc.depth, "main"))
		})
	}
}

func TestCloneDirName(t *testing.T) {
	repo := Repository{Name: "org/repo"}
	name := func(o RunOptions) string { return cloneDirName(repo, "key", o) }

	require.Equal(t, name(RunOptions{}), name(RunOptions{FetchDepth: 1}), "default equals depth 1")
	require.NotEqual(t, name(RunOptions{}), name(RunOptions{FetchDepth: 5}))
	require.NotEqual(t, name(RunOptions{FetchDepth: 5}), name(RunOptions{FetchDepth: 10}))
	require.NotEqual(t, name(RunOptions{}), name(RunOptions{FetchDepth: -1}))
	require.NotEqual(t,
		name(RunOptions{CloningSubset: []string{"a"}, FetchDepth: 5}),
		name(RunOptions{CloningSubset: []string{"a"}}),
	)
}

func TestCloneRepositoryFetchDepth(t *testing.T) {
	for depth, expected := range map[int]string{0: "--depth=1", 3: "--depth=3"} {
		var fetch []string
		overrideExecerFactory(t, func(string, *slog.Logger) exec.Execer {
			return mock.Execer{
				RunXFn: func(_ context.Context, command string, args ...string) (string, error) {
					if len(args) > 0 && args[0] == "fetch" {
						fetch = args
					}
					return "", nil
				},
			}
		})

		err := cloneRepository(context.Background(), Repository{SSHURL: "u", DefaultBranchName: "main"}, t.TempDir(), RunOptions{FetchDepth: depth})
		require.NoError(t, err)
		require.Equal(t, []string{"fetch", expected, "origin", "main"}, fetch)
	}
}
