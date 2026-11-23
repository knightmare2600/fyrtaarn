//go:build !windows

package ptyproc

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// Enter is what the Enter key sends: a newline on a Unix pty.
var Enter = []byte{'\n'}

// Available reports whether consoles can be started here.
func Available() bool { return true }

type unixConsole struct {
	*os.File // pty master
	cmd      *exec.Cmd
}

// Start runs cmd in a pty of cols×rows.
func Start(cmd *exec.Cmd, cols, rows int) (Console, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	return &unixConsole{File: f, cmd: cmd}, nil
}

func (c *unixConsole) Resize(cols, rows int) error {
	return pty.Setsize(c.File, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (c *unixConsole) Wait() error { return c.cmd.Wait() }

func (c *unixConsole) StopSteps() []StopStep {
	sig := func(s syscall.Signal) func() error {
		return func() error { return c.cmd.Process.Signal(s) }
	}
	return []StopStep{
		{"SIGINT", Interrupt, sig(syscall.SIGINT)},
		{"SIGTERM", Terminate, sig(syscall.SIGTERM)},
		{"SIGKILL", Kill, sig(syscall.SIGKILL)},
	}
}
