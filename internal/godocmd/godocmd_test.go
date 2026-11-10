package godocmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	dir := t.TempDir()
	src := `// Package p does things.
package p

// Answer is the answer.
const Answer = 42

// T is a type.
type T struct {
	// A is exported.
	A int
	b int
}

// New creates a T.
func New() T { return T{} }

// Do does it.
func (t T) Do() {}

func hidden() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p_test.go"), []byte("package p\n\nfunc Ignored() {}\n"), 0o600))

	got, err := Render(dir, "example.com/p")
	require.NoError(t, err)

	require.Contains(t, got, "Package p does things.")
	require.Contains(t, got, "const Answer = 42")
	require.Contains(t, got, "#### type T")
	require.Contains(t, got, "// A is exported.")
	require.NotContains(t, got, "b int")
	require.Contains(t, got, "#### func New")
	require.Contains(t, got, "#### method (T) Do")
	require.NotContains(t, got, "hidden")
	require.NotContains(t, got, "Ignored")
}
