package mock

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// CallIs is a helper function for tests that checks if the given command and arguments match the expected values.
// It returns true if the command and arguments match, and false otherwise.
// The first element of expected is the expected command, and the remaining elements are the expected arguments.
// If an expected argument is CallAny, it will match any value for that argument position.
func CallIs(t *testing.T, cmd string, args []string, expected ...any) bool {
	t.Helper()

	if len(expected) > len(args)+1 {
		return false
	}

	if cmd != expected[0] {
		return false
	}

	for i, xArg := range expected[1:] {
		switch txArg := xArg.(type) {
		case CallArg:
			if txArg == nil {
				continue
			}

			if !txArg(args[i]) {
				t.Log("argument mismatch at position", i, "expected condition to be true, actual:", args[i])
				return false
			}
		case string:
			if txArg != args[i] {
				t.Log("argument mismatch at position", i, "expected:", txArg, "actual:", args[i])
				return false
			}
		default:
			require.Fail(t, "expected argument must be a string or condition")
		}
	}

	return true
}

type CallArg func(string) bool

var (
	// CallAny is a sentinel value used in tests to indicate that any value is acceptable for a given argument position.
	CallAny CallArg = nil

	// ErrUnexpectedCall is an error returned by the mock Execer when an unexpected command is called.
	ErrUnexpectedCall = errors.New("unexpected call")
)
