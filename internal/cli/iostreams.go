package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"golang.org/x/term"
)

// IOStreams groups the standard streams a command reads and writes.
//
// Human-readable output and machine output both go to Out (a single command
// never mixes them); warnings, progress and log lines go to ErrOut so
// `leptostack ... -o json | jq` is always safe.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	inFile *bufio.Reader
}

// NewIOStreams builds IOStreams from raw readers/writers.
func NewIOStreams(in io.Reader, out, errOut io.Writer) *IOStreams {
	return &IOStreams{In: in, Out: out, ErrOut: errOut}
}

func (s *IOStreams) reader() *bufio.Reader {
	if s.inFile == nil {
		s.inFile = bufio.NewReader(s.In)
	}
	return s.inFile
}

func fdOf(v any) (uintptr, bool) {
	f, ok := v.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return f.Fd(), true
}

// IsStdinTTY reports whether stdin is a terminal.
func (s *IOStreams) IsStdinTTY() bool {
	fd, ok := fdOf(s.In)
	return ok && term.IsTerminal(int(fd))
}

// IsStdoutTTY reports whether stdout is a terminal.
func (s *IOStreams) IsStdoutTTY() bool {
	fd, ok := fdOf(s.Out)
	return ok && term.IsTerminal(int(fd))
}

// IsStderrTTY reports whether stderr is a terminal.
func (s *IOStreams) IsStderrTTY() bool {
	fd, ok := fdOf(s.ErrOut)
	return ok && term.IsTerminal(int(fd))
}

// ReadLine reads a single line from stdin, trimming the trailing newline.
func (s *IOStreams) ReadLine() (string, error) {
	line, err := s.reader().ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// ReadSecret reads a line without echoing when stdin is a TTY.
func (s *IOStreams) ReadSecret() (string, error) {
	fd, ok := fdOf(s.In)
	if ok && term.IsTerminal(int(fd)) {
		b, err := term.ReadPassword(int(fd))
		fmt.Fprintln(s.Out)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return s.ReadLine()
}

// Printf writes formatted human output to Out.
func (s *IOStreams) Printf(format string, args ...any) {
	fmt.Fprintf(s.Out, format, args...)
}

// Println writes a line to Out.
func (s *IOStreams) Println(args ...any) {
	fmt.Fprintln(s.Out, args...)
}

// Errf writes formatted output to ErrOut (progress, warnings, logs).
func (s *IOStreams) Errf(format string, args ...any) {
	fmt.Fprintf(s.ErrOut, format, args...)
}

// Errln writes a line to ErrOut.
func (s *IOStreams) Errln(args ...any) {
	fmt.Fprintln(s.ErrOut, args...)
}
