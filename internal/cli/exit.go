package cli

import (
	"errors"
	"fmt"
)

// Exit codes documented in --help and the README.
const (
	ExitOK          = 0
	ExitFailure     = 1
	ExitUsage       = 2
	ExitConfig      = 3
	ExitPrereq      = 4
	ExitTimeout     = 5
	ExitUnreachable = 6
)

// ExitError carries an explicit process exit code alongside an error.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// Exitf builds an ExitError with a formatted message.
func Exitf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}

// WithExitCode wraps err so the process exits with code.
func WithExitCode(code int, err error) error {
	if err == nil {
		return &ExitError{Code: code}
	}
	return &ExitError{Code: code, Err: err}
}

// CodeOf extracts the exit code from an error, defaulting to ExitFailure.
func CodeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return ExitFailure
}
