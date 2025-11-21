package exec

import (
	"errors"
	"strings"
)

type execError struct {
	msg      string
	stderr   string
	exitCode int
}

func (e execError) Error() string {
	return e.msg
}

func (e execError) Stderr() string {
	return e.stderr
}

func (e execError) ExitCode() int {
	return e.exitCode
}

func NewExecErr(message string, stderr string, exitCode int) error {
	if exitCode == 0 {
		return nil
	}

	return execError{message, stderr, exitCode}
}

// GetStderr returns the stderr from a RunX error
func GetStderr(err error) (string, bool) {
	var execErr = execError{}
	if errors.As(err, &execErr) {
		return execErr.stderr, true
	}

	return "", false
}

// StderrNotEmpty checks if the stderr is truly not empty
func StderrNotEmpty(stderr string, ok bool) (string, bool) {
	if !ok || len(strings.TrimSpace(stderr)) == 0 {
		return "", false
	}

	return stderr, true
}
