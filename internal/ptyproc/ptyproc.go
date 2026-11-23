// Package ptyproc runs a command inside a pseudo-terminal: a Unix pty
// (creack/pty) or a Windows pseudo-console (ConPTY, charmbracelet/x/conpty,
// Windows 10 1809 / Server 2019 and later). fyrtaarn uses it for ipmitool's
// SOL console and for the password prompts of "user set password", which
// read from a terminal (getpass).
package ptyproc

import "io"

// Console is a running command and its pseudo-terminal.
type Console interface {
	io.Reader // the command's output; fails once it has gone and the console is closed
	io.Writer // keystrokes to the command
	Resize(cols, rows int) error
	// Close releases the console. Call only once the command has exited:
	// on Unix, closing a pty with a Read in flight does not end that Read.
	Close() error
	// Wait blocks until the command exits and returns its exit error, nil
	// for a clean exit. On Windows it also closes the console, as a ConPTY's
	// output does not end when the process does. Call it once.
	Wait() error
	// StopSteps are the escalating ways to end the command, gentlest first.
	StopSteps() []StopStep
}

// StopKind says how forceful a StopStep is, so callers can choose how long
// to wait after each.
type StopKind int

const (
	Interrupt StopKind = iota // SIGINT, or Ctrl+C written to the console on Windows
	Terminate                 // SIGTERM (Unix only)
	Kill                      // SIGKILL / TerminateProcess
)

// StopStep is one way to end the command.
type StopStep struct {
	Name string
	Kind StopKind
	Do   func() error
}

// KillNow runs c's last, unconditional stop step.
func KillNow(c Console) error {
	steps := c.StopSteps()
	return steps[len(steps)-1].Do()
}
