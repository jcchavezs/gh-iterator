package exec

type execErr struct {
	msg      string
	stderr   string
	exitCode int
}

func (e execErr) Error() string {
	return e.msg
}

func (e execErr) Stderr() string {
	return e.stderr
}

func (e execErr) ExitCode() int {
	return e.exitCode
}

func NewExecErr(message string, stderr string, exitCode int) error {
	if exitCode == 0 {
		return nil
	}

	return execErr{message, stderr, exitCode}
}

// ExecErr is the error returned by exec.RunX including the exit code and the Stderr content
type ExecErr interface {
	Error() string
	Stderr() string
	ExitCode() int
}
