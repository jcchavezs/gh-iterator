package require

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArgEqual(t *testing.T) {
	t.Run("successful comparison", func(t *testing.T) {
		args := []string{"arg1", "arg2", "arg3"}

		// Test each position
		ArgEqual(t, "arg1", args, 0)
		ArgEqual(t, "arg2", args, 1)
		ArgEqual(t, "arg3", args, 2)
	})

	t.Run("with custom message", func(t *testing.T) {
		args := []string{"cmd", "expected-value"}
		ArgEqual(t, "expected-value", args, 1, "custom message: value should match")
	})

	t.Run("fails when index is out of bounds", func(t *testing.T) {
		mockT := &mockTestingT{t: t}
		args := []string{"cmd", "arg1"}

		defer func() {
			require.True(t, mockT.isHelper, "Helper method should have been called")
			// This is a hack as due to mocking ArgEqual failure won't stop the execution
			// unless we specifically call SkipNow in our method mock.
			require.True(t, mockT.failed, "should have failed due to insufficient arguments")
		}()

		ArgEqual(mockT, "arg2", args, 2)
	})

	t.Run("fails when values don't match", func(t *testing.T) {
		mockT := &mockTestingT{t: t}
		args := []string{"cmd", "arg1", "arg2"}

		defer func() {
			require.True(t, mockT.isHelper, "Helper method should have been called")
			// This is a hack as due to mocking ArgEqual failure won't stop the execution
			// unless we specifically call SkipNow in our method mock.
			require.True(t, mockT.failed, "should have failed due to value mismatch")
		}()

		ArgEqual(mockT, "wrong-value", args, 1)
	})

	t.Run("with empty args slice", func(t *testing.T) {
		mockT := &mockTestingT{t: t}
		args := []string{}

		defer func() {
			require.True(t, mockT.isHelper, "Helper method should have been called")
			// This is a hack as due to mocking ArgEqual failure won't stop the execution
			// unless we specifically call SkipNow in our method mock.
			require.True(t, mockT.failed, "should have failed with empty args")
		}()

		ArgEqual(mockT, "anything", args, 0)
	})

	t.Run("with nil args slice", func(t *testing.T) {
		mockT := &mockTestingT{t: t}
		var args []string

		ArgEqual(mockT, "anything", args, 0)

		require.True(t, mockT.isHelper, "Helper method should have been called")
		require.True(t, mockT.failed, "should have failed with nil args")
	})
}

// mockTestingT is a mock implementation of require.TestingT for testing purposes
type mockTestingT struct {
	failed   bool
	isHelper bool
	t        *testing.T
}

func (m *mockTestingT) Errorf(format string, args ...any) {
	m.failed = true
}

func (m *mockTestingT) FailNow() {
	m.failed = true
	m.t.SkipNow()
}

func (m *mockTestingT) Helper() {
	m.isHelper = true
}
