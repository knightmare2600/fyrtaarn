package ipmi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/command/transport"
	"github.com/bougou/go-ipmi/pkg/types"

	"github.com/knightmare2600/fyrtaarn/internal/ptyproc"
)

// NativeSOL is a Serial-over-LAN console over fyrtaarn's own IPMI session,
// in place of "ipmitool sol activate" in a pty. It is a ptyproc.Console, so
// the SOL pane (terminal emulation, keys, scroll-back, teardown) drives it
// exactly as it drives ipmitool:
//   - Read gives the server's console output; Write sends keystrokes,
//     unchanged (go-ipmi's SOLStream, not its SOLActivate, which would end
//     the session on "~X" at the start of a line and take over Ctrl+C);
//   - the pane's clean exit, the escape character then '.', and its first
//     stop step both deactivate the payload and close the session;
//   - a session that ends on its own (activation refused, the BMC ending
//     SOL, the network) prints its reason as the last line and returns it
//     from Wait, as ipmitool's exit does. A refusal because another session
//     holds the payload uses ipmitool's wording ("SOL payload already
//     active on another session"), which the pane looks for to offer a
//     takeover.
//
// The session's keepalive is go-ipmi's own, every 10 s while idle.
type NativeSOL struct {
	c      *client.Client
	cancel context.CancelFunc
	inR    *io.PipeReader
	inW    *io.PipeWriter
	out    *solOutput
	done   chan struct{}
	err    error
	stop   sync.Once
	lineSt bool // last byte written was CR/LF, or nothing written yet
}

// solActivateTimeout bounds activation; the console itself has no limit.
const solActivateTimeout = 15 * time.Second

// StartNativeSOL opens a session with host and starts the console. It
// returns errNativeOff when host is to use ipmitool instead (backend
// choice, or a session that could not be set up: the host then uses
// ipmitool from now on, as for every other command).
func StartNativeSOL(host, user, pass string) (*NativeSOL, error) {
	if !useNative(host) {
		return nil, errNativeOff
	}
	c, err := client.NewClient(host, 623, user, pass)
	if err == nil {
		c.WithMaxPrivilegeLevel(types.PrivilegeLevelAdministrator).WithTimeout(4 * time.Second).WithRetry(1)
		if wantSuite3(host) {
			c.WithCipherSuiteID(types.CipherSuiteID3)
		}
		cctx, ccancel := context.WithTimeout(context.Background(), ipmiShortTimeout)
		err = c.Connect(cctx)
		ccancel()
	}
	if err != nil {
		if !fallbackAllowed() {
			return nil, fmt.Errorf("native IPMI session: %w", err)
		}
		nativeFailed.Store(host, err.Error())
		return nil, errNativeOff
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &NativeSOL{c: c, cancel: cancel, done: make(chan struct{}), lineSt: true}
	s.inR, s.inW = io.Pipe()
	s.out = newSOLOutput()
	go s.run(ctx)
	return s, nil
}

// activateSOL activates instance 1, asking for encryption and
// authentication of the SOL data, as the session has them on every
// cipher suite fyrtaarn uses (3 and the library's default choice). A BMC
// that refuses that combination (0x83) is asked again without.
func activateSOL(ctx context.Context, c *client.Client) error {
	req := &transport.ActivatePayloadRequest{PayloadType: types.PayloadTypeSOL, PayloadInstance: 1, EnableEncryption: true, EnableAuthentication: true}
	_, err := c.ActivatePayload(ctx, req)
	if r, ok := types.IsResponseError(err); ok && r.CompletionCode() == 0x83 {
		req.EnableEncryption, req.EnableAuthentication = false, false
		_, err = c.ActivatePayload(ctx, req)
	}
	return err
}

// solActivateError words an activation failure as ipmitool does where the
// pane depends on it.
func solActivateError(err error) error {
	if r, ok := types.IsResponseError(err); ok {
		switch r.CompletionCode() {
		case 0x80:
			return errors.New("Info: SOL payload already active on another session")
		case 0x81:
			return errors.New("Info: SOL payload disabled")
		case 0x82:
			return errors.New("Info: SOL payload activation limit reached")
		}
	}
	return fmt.Errorf("Error activating SOL payload: %w", err)
}

func (s *NativeSOL) run(ctx context.Context) {
	defer close(s.done)
	actx, acancel := context.WithTimeout(ctx, solActivateTimeout)
	err := activateSOL(actx, s.c)
	acancel()
	if err != nil {
		s.finish(solActivateError(err), false)
		return
	}
	// Instance 1 is ours now; note which session the BMC says owns it, so
	// the watch below can tell when another session has taken it over.
	owner, oerr := payloadOwner(ctx, s.c)
	_, _ = io.WriteString(s.out, "[SOL Session operational (fyrtaarn's own IPMI). F10 disconnects.]\r\n")
	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	lostCh := make(chan error, 1) // the watch's verdict, always sent once
	if oerr == nil {
		go func() {
			lost := watchPayload(sctx, s.c, owner)
			if lost != nil {
				scancel()
			}
			lostCh <- lost
		}()
	} else {
		lostCh <- nil
	}
	err = s.c.SOLStream(sctx, s.inR, s.out)
	scancel()
	lost := <-lostCh
	switch {
	case lost != nil:
		s.finish(lost, false) // no longer ours to deactivate
		return
	case err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled):
		err = fmt.Errorf("SOL session ended: %w", err)
	default:
		err = nil // ended by the user
	}
	s.finish(err, true)
}

// solWatchInterval is how often the console checks it still holds the
// payload. Nothing else would tell it: on an idle console no SOL packets
// flow, so a takeover by another session (its "sol deactivate" and its own
// activation) went unnoticed on the Quanta, the pane showing a live
// console that no longer was.
var solWatchInterval = 5 * time.Second

// ErrSOLTakenOver ends a console whose payload another session took.
var ErrSOLTakenOver = errors.New("SOL session ended: another session deactivated or took over the SOL payload")

func payloadOwner(ctx context.Context, c *client.Client) (uint32, error) {
	r, err := c.GetPayloadInstanceInfo(ctx, types.PayloadTypeSOL, 1)
	if err != nil {
		return 0, err
	}
	return r.SessionID, nil
}

// watchPayload returns ErrSOLTakenOver once instance 1 is inactive or
// owned by another session, nil when ctx ends. A check that fails is not
// taken as a loss: the keepalive and the stream report a dead session.
func watchPayload(ctx context.Context, c *client.Client, owner uint32) error {
	t := time.NewTicker(solWatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		st, err := c.GetPayloadActivationStatus(ctx, types.PayloadTypeSOL)
		if err != nil {
			continue
		}
		if !st.Instance01Activated {
			return ErrSOLTakenOver
		}
		if now, err := payloadOwner(ctx, c); err == nil && now != owner {
			return ErrSOLTakenOver
		}
	}
}

// finish deactivates the payload (if it was activated), closes the
// session, reports err as the last line of output, and ends Read.
func (s *NativeSOL) finish(err error, active bool) {
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()
	if active {
		_, _ = s.c.DeactivatePayload(cctx, &transport.DeactivatePayloadRequest{PayloadType: types.PayloadTypeSOL, PayloadInstance: 1})
	}
	_ = s.c.Close(cctx)
	if err != nil {
		_, _ = io.WriteString(s.out, "\r\n"+err.Error()+"\r\n")
	}
	s.err = err
	_ = s.out.Close()
}

// end asks the session to finish: input EOF makes SOLStream flush and
// return; the context is the backstop if the BMC stops answering.
func (s *NativeSOL) end() error {
	s.stop.Do(func() {
		_ = s.inW.Close()
		go func() {
			select {
			case <-s.done:
			case <-time.After(5 * time.Second):
				s.cancel()
			}
		}()
	})
	return nil
}

func (s *NativeSOL) Read(p []byte) (int, error) { return s.out.Read(p) }

// Write sends keystrokes. The pane's clean-exit sequence (SOLEscapeChar
// then '.', at the start of a line, which only the pane itself sends: the
// key mapping cannot produce SOLEscapeChar) ends the session instead.
func (s *NativeSOL) Write(p []byte) (int, error) {
	if s.lineSt && len(p) == 2 && p[0] == SOLEscapeChar && p[1] == '.' {
		return len(p), s.end()
	}
	if len(p) > 0 {
		s.lineSt = p[len(p)-1] == '\r' || p[len(p)-1] == '\n'
	}
	return s.inW.Write(p)
}

// Resize does nothing: SOL carries no window size.
func (s *NativeSOL) Resize(cols, rows int) error { return nil }

// Close ends the session if it is still running and releases the output.
func (s *NativeSOL) Close() error {
	_ = s.end()
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		s.cancel()
	}
	return s.out.Close()
}

// Wait blocks until the session has ended and returns why, nil when ended
// by the user.
func (s *NativeSOL) Wait() error {
	<-s.done
	return s.err
}

// StopSteps has one step: deactivate and close, as the escape does.
func (s *NativeSOL) StopSteps() []ptyproc.StopStep {
	return []ptyproc.StopStep{{Name: "close SOL session", Kind: ptyproc.Interrupt, Do: s.end}}
}

var _ ptyproc.Console = (*NativeSOL)(nil)

// IsNativeOff reports whether err means "use ipmitool for this instead".
func IsNativeOff(err error) bool { return errors.Is(err, errNativeOff) }

func nativeSOLDeactivate(host, user, pass string) error {
	return withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		_, err := c.DeactivatePayload(ctx, &transport.DeactivatePayloadRequest{PayloadType: types.PayloadTypeSOL, PayloadInstance: 1})
		return err
	})
}

// solOutput is the console output on its way to Read. Unlike io.Pipe, a
// Write never waits for a reader, so the session can always finish (write
// its reason, deactivate, close) even if nothing is reading any more.
type solOutput struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newSOLOutput() *solOutput {
	o := &solOutput{}
	o.cond = sync.NewCond(&o.mu)
	return o
}

func (o *solOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, io.ErrClosedPipe
	}
	o.buf = append(o.buf, p...)
	o.cond.Broadcast()
	return len(p), nil
}

// Read waits for output; io.EOF once closed and drained.
func (o *solOutput) Read(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.buf) == 0 && !o.closed {
		o.cond.Wait()
	}
	if len(o.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, o.buf)
	o.buf = o.buf[n:]
	return n, nil
}

// Close ends Read once what was written has been read.
func (o *solOutput) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.cond.Broadcast()
	return nil
}
