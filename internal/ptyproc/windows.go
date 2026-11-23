//go:build windows

package ptyproc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/charmbracelet/x/conpty"
	"golang.org/x/sys/windows"
)

// winConsole runs ipmitool in a Windows pseudo-console (ConPTY, Windows 10
// 1809 / Server 2019 and later).
type winConsole struct {
	pty       *conpty.ConPty
	proc      *os.Process
	closeOnce sync.Once
	closeErr  error
}

// Enter is what the Enter key sends on a Windows console: a carriage
// return (a line feed would be Ctrl+J).
var Enter = []byte{'\r'}

// Available reports whether this Windows has ConPTY. Older releases (8.1,
// Server 2016) lack CreatePseudoConsole.
func Available() bool {
	return windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find() == nil
}

// Start runs cmd in a pseudo-console of cols×rows. The process gets the
// command line and environment exec.Cmd was built with; cmd itself is not
// started, and its context is not watched (callers kill on timeout).
func Start(cmd *exec.Cmd, cols, rows int) (Console, error) {
	if cmd.Err != nil {
		return nil, cmd.Err // e.g. ipmitool.exe not found on PATH
	}
	c, err := conpty.New(cols, rows, 0)
	if err != nil {
		return nil, err
	}
	pid, handle, err := c.Spawn(cmd.Path, cmd.Args, &syscall.ProcAttr{Dir: cmd.Dir, Env: cmd.Env})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("starting %s: %w", cmd.Path, err)
	}
	// os.FindProcess opens its own handle; take it before releasing the
	// one Spawn returned, so the process cannot be lost in between.
	proc, err := os.FindProcess(pid)
	_ = windows.CloseHandle(windows.Handle(handle))
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return &winConsole{pty: c, proc: proc}, nil
}

func (c *winConsole) Read(p []byte) (int, error) {
	n, err := c.pty.Read(p)
	if n == 0 && err == nil {
		// ReadFile on a closed pipe can report 0 bytes and no error.
		return 0, errors.New("pseudo-console closed")
	}
	return n, err
}

func (c *winConsole) Write(p []byte) (int, error) { return c.pty.Write(p) }

func (c *winConsole) Resize(cols, rows int) error { return c.pty.Resize(cols, rows) }

func (c *winConsole) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.pty.Close() })
	return c.closeErr
}

// Wait waits for the command, then closes the pseudo-console. Unlike a Unix pty,
// a ConPTY's output pipe does not end when the process exits; reads keep
// blocking until ClosePseudoConsole, so without this listenSOL would never
// see the session end.
func (c *winConsole) Wait() error {
	state, err := c.proc.Wait()
	_ = c.Close()
	if err != nil {
		return err
	}
	if !state.Success() {
		return errors.New(state.String()) // "exit status N", as exec.ExitError says
	}
	return nil
}

// StopSteps: Ctrl+C written into the pseudo-console becomes a CTRL_C_EVENT
// for ipmitool, the Windows counterpart of SIGINT; then TerminateProcess.
// Windows console programs have no SIGTERM.
func (c *winConsole) StopSteps() []StopStep {
	return []StopStep{
		{"Ctrl+C", Interrupt, func() error { _, err := c.pty.Write([]byte{0x03}); return err }},
		{"TerminateProcess", Kill, c.proc.Kill},
	}
}
