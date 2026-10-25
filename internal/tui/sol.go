package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

/* ---------------- MESSAGES ---------------- */

type solPaneReadyMsg struct {
	pane *solPane
}

// solReadMsg carries one pty read. Gen identifies the session it came from
// so a read still in flight from a closed session is discarded instead of
// being fed into (and starting a second reader on) a newer session's pane.
type solReadMsg struct {
	Data []byte
	Err  error
	Gen  int
}

// solDoneMsg reports that the pty reached EOF because ipmitool exited on its
// own (BMC closed the session, activation refused, keepalive lost). gen
// guards against messages from a session the user already closed; exitErr
// is ipmitool's exit status.
type solDoneMsg struct {
	gen     int
	exitErr error
}

// solClosedMsg reports that a session's teardown (see solPane.shutdown) has
// finished: ipmitool has exited, been reaped, and the pty is closed.
type solClosedMsg struct {
	host string
	err  error // non-nil only if the fallback "sol deactivate" failed
}

/* ---------------- CONSTANTS ---------------- */

const (
	solHistoryMax  = 1000
	solDefaultCols = 80
	solDefaultRows = 24

	stateNormal  = 0
	stateEscape  = 1
	stateCharset = 2 // after ESC ( / ) — consume one more byte then return
	stateCSI     = 3
	stateOSC     = 4
	stateSSThree = 5 // after ESC O — SS3: app cursor keys (A-D) and F1-F4 (P-S)
)

// Teardown timeouts for each step of solPane.shutdown. Variables rather than
// constants so tests can shorten them.
var (
	solEscapeWait = 3 * time.Second // ipmitool deactivates + closes session itself
	solSigintWait = 5 * time.Second // SIGINT handler sends Close Session (one retry)
	solKillWait   = 2 * time.Second // per SIGTERM / SIGKILL step
)

/* ---------------- PANE ---------------- */

// solPane is a VT100-compatible screen buffer. It maintains a cols×rows grid
// of runes, tracks cursor position, and processes escape sequences so that
// applications using cursor positioning (GRUB, vim, htop) render correctly.
type solPane struct {
	ptmx *os.File
	cols int
	rows int

	// cmd is the ipmitool process; exited is closed once cmd.Wait() returns,
	// after exitErr has been set.
	cmd     *exec.Cmd
	exited  chan struct{}
	exitErr error

	// lastSentCR tracks whether the last byte written to ipmitool was CR/LF
	// (or nothing has been written). ipmitool only honours its escape
	// character at the start of a line, so shutdown can only use the clean
	// escape + '.' exit when this is true.
	lastSentCR bool

	// Screen grid: grid[row][col], space-filled.
	grid   [][]rune
	curRow int
	curCol int

	// Scroll region (0-indexed, inclusive). Defaults to full screen.
	scrollTop    int
	scrollBottom int

	// Saved cursor (ESC 7 / ESC 8 / CSI s / CSI u).
	savedRow int
	savedCol int

	// VT parser state machine.
	state      int
	csiParams  []int
	csiPrivate bool
	csiInter   byte

	// History: lines scrolled off the top of the screen (oldest first).
	history []string

	// scrollUp is the user's scroll-back offset (0 = live bottom of output).
	scrollUp int

	// editBuf shadows what the user has typed into the current readline command.
	// It lets updateSOL know there is something to the left of the cursor to
	// delete, so we can send ESC[D (cursor-left) + \x04 (Ctrl+D, delete-forward)
	// instead of a backspace byte that GRUB may ignore or misinterpret.
	editBuf []rune
}

func newSolPane(ptmx *os.File, cols, rows int) *solPane {
	if cols < 1 {
		cols = solDefaultCols
	}
	if rows < 1 {
		rows = solDefaultRows
	}
	return &solPane{
		ptmx:         ptmx,
		cols:         cols,
		rows:         rows,
		grid:         makeGrid(rows, cols),
		scrollBottom: rows - 1,
		csiParams:    make([]int, 0, 8),
		lastSentCR:   true,
	}
}

// startSolProcess runs cmd inside a pty and returns a pane that owns both.
// A goroutine reaps the process so it never lingers as a zombie and so
// shutdown and listenSOL can tell when it has gone.
func startSolProcess(cmd *exec.Cmd, cols, rows int) (*solPane, error) {
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(max(rows, solDefaultRows)),
		Cols: uint16(max(cols, solDefaultCols)),
	})
	if err != nil {
		return nil, err
	}
	p := newSolPane(ptmx, cols, rows)
	p.cmd = cmd
	p.exited = make(chan struct{})
	go func() {
		p.exitErr = cmd.Wait()
		close(p.exited)
	}()
	return p, nil
}

func makeGrid(rows, cols int) [][]rune {
	grid := make([][]rune, rows)
	for r := range grid {
		grid[r] = make([]rune, cols)
		for c := range grid[r] {
			grid[r][c] = ' '
		}
	}
	return grid
}

func (s *solPane) resize(cols, rows int) {
	_ = pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: uint16(max(rows, 1)),
		Cols: uint16(max(cols, 1)),
	})
	newGrid := makeGrid(rows, cols)
	for r := 0; r < rows && r < s.rows; r++ {
		for c := 0; c < cols && c < s.cols; c++ {
			newGrid[r][c] = s.grid[r][c]
		}
	}
	s.grid = newGrid
	s.cols = cols
	s.rows = rows
	s.curRow = min(s.curRow, rows-1)
	s.curCol = min(s.curCol, cols-1)
	s.scrollBottom = min(s.scrollBottom, rows-1)
}

func (s *solPane) write(b []byte) {
	if len(b) == 0 {
		return
	}
	_, _ = s.ptmx.Write(b)
	last := b[len(b)-1]
	s.lastSentCR = last == '\r' || last == '\n'
}

// waitExit reports whether ipmitool has exited within d.
func (s *solPane) waitExit(d time.Duration) bool {
	select {
	case <-s.exited:
		return true
	case <-time.After(d):
		return false
	}
}

// shutdown ends the ipmitool process and only then closes the pty.
//
// Closing the pty first does nothing: pty.Open calls Fd(), which leaves the
// master in blocking mode, and Go's os.File.Close on a blocking fd defers the
// real close(2) until the in-flight Read in listenSOL returns. On an idle
// console that never happens, so ipmitool kept running — and kept the BMC's
// single SOL slot alive with its keepalives — after F10.
//
// Escalation: escape + '.' (ipmitool deactivates the payload and closes its
// session; only valid at the start of a line), then SIGINT (ipmitool's
// handler sends Close Session), then SIGTERM, then SIGKILL. Returns true if
// a signal was needed, meaning the payload may still be marked active and
// the caller is to send an explicit "sol deactivate". A process that had
// already exited on its own returns false — there is nothing of ours left
// to release.
func (s *solPane) shutdown() (needDeactivate bool) {
	select {
	case <-s.exited:
		_ = s.ptmx.Close()
		return false
	default:
	}

	// Nothing reads the pty once the pane is detached from the UI. Drain it
	// so ipmitool cannot block writing console output (or its own exit
	// messages) into a full pty buffer while we wait for it to exit. The
	// copy ends with EIO once ipmitool has gone.
	go func() { _, _ = io.Copy(io.Discard, s.ptmx) }()

	if s.lastSentCR {
		s.write([]byte{ipmi.SOLEscapeChar, '.'})
		if s.waitExit(solEscapeWait) {
			_ = s.ptmx.Close()
			return s.exitErr != nil
		}
	}

	needDeactivate = true
	steps := []struct {
		sig  syscall.Signal
		wait time.Duration
	}{
		{syscall.SIGINT, solSigintWait},
		{syscall.SIGTERM, solKillWait},
		{syscall.SIGKILL, solKillWait},
	}
	for _, st := range steps {
		_ = s.cmd.Process.Signal(st.sig)
		if s.waitExit(st.wait) {
			break
		}
	}
	_ = s.ptmx.Close()
	return needDeactivate
}

// lastLine returns the last non-blank line of output (screen, then
// history). When ipmitool exits on its own this is its reason, e.g.
// "Info: SOL payload already active on another session".
func (s *solPane) lastLine() string {
	for r := s.rows - 1; r >= 0; r-- {
		if line := strings.TrimSpace(rowToString(s.grid[r])); line != "" {
			return line
		}
	}
	for i := len(s.history) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(s.history[i]); line != "" {
			return line
		}
	}
	return ""
}

/* ---------------- VT100 PARSER ---------------- */

// ingest feeds raw pty output through the VT100 state machine.
func (s *solPane) ingest(raw []byte) {
	for len(raw) > 0 {
		b := raw[0]
		// In normal state, decode multi-byte UTF-8 runes (box-drawing glyphs etc).
		if s.state == stateNormal && b >= 0x80 {
			r, size := utf8.DecodeRune(raw)
			if r != utf8.RuneError && size > 1 {
				s.putChar(r)
				raw = raw[size:]
				continue
			}
		}
		s.processByte(b)
		raw = raw[1:]
	}
}

func (s *solPane) processByte(b byte) {
	switch s.state {
	case stateNormal:
		s.processNormal(b)
	case stateEscape:
		s.processEscape(b)
	case stateCharset:
		s.state = stateNormal
	case stateCSI:
		s.processCSI(b)
	case stateOSC:
		s.processOSC(b)
	case stateSSThree:
		s.processSSThree(b)
	}
}

func (s *solPane) processNormal(b byte) {
	switch {
	case b == 0x1b:
		s.state = stateEscape

	case b == '\r':
		// Pure carriage return: move to column 0, stay on the same row.
		// BIOS and firmware use bare \r to overwrite a progress line in-place;
		// treating it as CR+LF would generate a new line for every update and
		// scroll the splash screen into history. This matches xterm / real VT100.
		s.curCol = 0

	case b == '\n', b == 0x0b, b == 0x0c: // LF / VT / FF
		s.doLineFeed()

	case b == '\b': // BS — destructive: erase the character to the left
		if s.curCol > 0 {
			s.curCol--
			s.grid[s.curRow][s.curCol] = ' '
		}

	case b == 0x7f: // DEL — also destructive backspace (echoed by BIOS/serial on erase)
		if s.curCol > 0 {
			s.curCol--
			s.grid[s.curRow][s.curCol] = ' '
		}

	case b == '\t':
		next := ((s.curCol / 8) + 1) * 8
		if next >= s.cols {
			next = s.cols - 1
		}
		s.curCol = next

	case b == 0x07: // BEL — ignore
	case b == 0x0e, b == 0x0f: // SO / SI — charset shift, ignore

	case b >= 0x20:
		s.putChar(rune(b))
	}
}

func (s *solPane) processEscape(b byte) {
	s.state = stateNormal
	switch b {
	case '[':
		s.state = stateCSI
		s.csiParams = s.csiParams[:0]
		s.csiPrivate = false
		s.csiInter = 0
	case ']':
		s.state = stateOSC
	case 'O': // SS3 — application cursor/function keys
		s.state = stateSSThree
	case '(', ')', '*', '+':
		s.state = stateCharset
	case '7':
		s.savedRow, s.savedCol = s.curRow, s.curCol
	case '8':
		s.curRow = clampInt(s.savedRow, 0, s.rows-1)
		s.curCol = clampInt(s.savedCol, 0, s.cols-1)
	case 'D':
		s.doLineFeed()
	case 'E':
		s.curCol = 0
		s.doLineFeed()
	case 'M': // reverse index
		if s.curRow == s.scrollTop {
			s.scrollRegionDown()
		} else if s.curRow > 0 {
			s.curRow--
		}
	case 'c':
		s.fullReset()
	}
}

func (s *solPane) processCSI(b byte) {
	switch {
	case b >= 0x40 && b <= 0x7e:
		s.executeCSI(b)
		s.state = stateNormal
	case b == '?':
		s.csiPrivate = true
	case b >= 0x20 && b <= 0x2f:
		s.csiInter = b
	case b >= '0' && b <= '9':
		if len(s.csiParams) == 0 {
			s.csiParams = append(s.csiParams, 0)
		}
		s.csiParams[len(s.csiParams)-1] = s.csiParams[len(s.csiParams)-1]*10 + int(b-'0')
	case b == ';':
		s.csiParams = append(s.csiParams, 0)
	}
}

func (s *solPane) processOSC(b byte) {
	if b == 0x07 {
		s.state = stateNormal
	} else if b == 0x1b {
		s.state = stateEscape
	}
}

// processSSThree handles the byte following ESC O (SS3). In application cursor
// mode A-D move the cursor one step; P-S are F1-F4 and are silently consumed.
func (s *solPane) processSSThree(b byte) {
	s.state = stateNormal
	switch b {
	case 'A':
		s.curRow = max(s.curRow-1, s.scrollTop)
	case 'B':
		s.curRow = min(s.curRow+1, s.scrollBottom)
	case 'C':
		s.curCol = min(s.curCol+1, s.cols-1)
	case 'D':
		s.curCol = max(s.curCol-1, 0)
		// P, Q, R, S = F1-F4 in application mode — no action needed
	}
}

func (s *solPane) param(n, def int) int {
	if n < len(s.csiParams) && s.csiParams[n] != 0 {
		return s.csiParams[n]
	}
	return def
}

func (s *solPane) executeCSI(final byte) {
	switch final {
	case 'A':
		s.curRow = max(s.curRow-s.param(0, 1), s.scrollTop)
	case 'B':
		s.curRow = min(s.curRow+s.param(0, 1), s.scrollBottom)
	case 'C':
		s.curCol = min(s.curCol+s.param(0, 1), s.cols-1)
	case 'D':
		s.curCol = max(s.curCol-s.param(0, 1), 0)
	case 'E':
		s.curRow = min(s.curRow+s.param(0, 1), s.rows-1)
		s.curCol = 0
	case 'F':
		s.curRow = max(s.curRow-s.param(0, 1), 0)
		s.curCol = 0
	case 'G':
		s.curCol = clampInt(s.param(0, 1)-1, 0, s.cols-1)
	case 'H', 'f':
		s.curRow = clampInt(s.param(0, 1)-1, 0, s.rows-1)
		s.curCol = clampInt(s.param(1, 1)-1, 0, s.cols-1)
	case 'J':
		switch s.param(0, 0) {
		case 0:
			s.eraseFromCursorToEnd()
		case 1:
			s.eraseFromStartToCursor()
		case 2, 3:
			s.eraseDisplay()
		}
	case 'K':
		switch s.param(0, 0) {
		case 0:
			for c := s.curCol; c < s.cols; c++ {
				s.grid[s.curRow][c] = ' '
			}
		case 1:
			for c := 0; c <= s.curCol; c++ {
				s.grid[s.curRow][c] = ' '
			}
		case 2:
			for c := range s.grid[s.curRow] {
				s.grid[s.curRow][c] = ' '
			}
		}
	case 'L':
		n := s.param(0, 1)
		for i := 0; i < n; i++ {
			s.insertLineAt(s.curRow)
		}
	case 'M':
		n := s.param(0, 1)
		for i := 0; i < n; i++ {
			s.deleteLineAt(s.curRow)
		}
	case 'P':
		n := s.param(0, 1)
		if s.curCol+n > s.cols {
			n = s.cols - s.curCol
		}
		row := s.grid[s.curRow]
		copy(row[s.curCol:], row[s.curCol+n:])
		for c := s.cols - n; c < s.cols; c++ {
			row[c] = ' '
		}
	case 'S':
		n := s.param(0, 1)
		for i := 0; i < n; i++ {
			s.scrollRegionUp()
		}
	case 'T':
		n := s.param(0, 1)
		for i := 0; i < n; i++ {
			s.scrollRegionDown()
		}
	case 'd':
		s.curRow = clampInt(s.param(0, 1)-1, 0, s.rows-1)
	case 'r':
		top := clampInt(s.param(0, 1)-1, 0, s.rows-1)
		bot := clampInt(s.param(1, s.rows)-1, 0, s.rows-1)
		if top < bot {
			s.scrollTop = top
			s.scrollBottom = bot
			s.curRow, s.curCol = 0, 0
		}
	case 's':
		s.savedRow, s.savedCol = s.curRow, s.curCol
	case 'u':
		s.curRow = clampInt(s.savedRow, 0, s.rows-1)
		s.curCol = clampInt(s.savedCol, 0, s.cols-1)
	case 'm': // SGR — strip colour/attributes
	case 'h', 'l': // mode set/reset — ignore
	}
}

/* ---------------- SCREEN OPERATIONS ---------------- */

func (s *solPane) putChar(r rune) {
	if s.curCol >= s.cols {
		s.curCol = 0
		s.doLineFeed()
	}
	s.grid[s.curRow][s.curCol] = r
	s.curCol++
}

func (s *solPane) doLineFeed() {
	if s.curRow == s.scrollBottom {
		s.scrollRegionUp()
	} else if s.curRow < s.rows-1 {
		s.curRow++
	}
}

func (s *solPane) scrollRegionUp() {
	if s.scrollTop == 0 {
		s.history = append(s.history, rowToString(s.grid[0]))
		if len(s.history) > solHistoryMax {
			s.history = s.history[len(s.history)-solHistoryMax:]
		}
	}
	for r := s.scrollTop; r < s.scrollBottom; r++ {
		copy(s.grid[r], s.grid[r+1])
	}
	for c := range s.grid[s.scrollBottom] {
		s.grid[s.scrollBottom][c] = ' '
	}
}

func (s *solPane) scrollRegionDown() {
	for r := s.scrollBottom; r > s.scrollTop; r-- {
		copy(s.grid[r], s.grid[r-1])
	}
	for c := range s.grid[s.scrollTop] {
		s.grid[s.scrollTop][c] = ' '
	}
}

func (s *solPane) eraseDisplay() {
	for r := range s.grid {
		for c := range s.grid[r] {
			s.grid[r][c] = ' '
		}
	}
}

func (s *solPane) eraseFromCursorToEnd() {
	for c := s.curCol; c < s.cols; c++ {
		s.grid[s.curRow][c] = ' '
	}
	for r := s.curRow + 1; r < s.rows; r++ {
		for c := range s.grid[r] {
			s.grid[r][c] = ' '
		}
	}
}

func (s *solPane) eraseFromStartToCursor() {
	for r := 0; r < s.curRow; r++ {
		for c := range s.grid[r] {
			s.grid[r][c] = ' '
		}
	}
	for c := 0; c <= s.curCol && c < s.cols; c++ {
		s.grid[s.curRow][c] = ' '
	}
}

func (s *solPane) insertLineAt(row int) {
	if row > s.scrollBottom {
		return
	}
	for r := s.scrollBottom; r > row; r-- {
		copy(s.grid[r], s.grid[r-1])
	}
	for c := range s.grid[row] {
		s.grid[row][c] = ' '
	}
}

func (s *solPane) deleteLineAt(row int) {
	if row > s.scrollBottom {
		return
	}
	for r := row; r < s.scrollBottom; r++ {
		copy(s.grid[r], s.grid[r+1])
	}
	for c := range s.grid[s.scrollBottom] {
		s.grid[s.scrollBottom][c] = ' '
	}
}

func (s *solPane) fullReset() {
	s.grid = makeGrid(s.rows, s.cols)
	s.curRow, s.curCol = 0, 0
	s.scrollTop, s.scrollBottom = 0, s.rows-1
	s.savedRow, s.savedCol = 0, 0
}

/* ---------------- RENDERING / LOG HELPERS ---------------- */

// allLines returns history + current screen rows for the renderer.
func (s *solPane) allLines() []string {
	out := make([]string, 0, len(s.history)+s.rows)
	out = append(out, s.history...)
	for _, row := range s.grid {
		out = append(out, rowToString(row))
	}
	return out
}

// screenLines returns only the current live screen rows as strings.
// Used when writing the final screen state to the session log.
func (s *solPane) screenLines() []string {
	lines := make([]string, s.rows)
	for i, row := range s.grid {
		lines[i] = rowToString(row)
	}
	return lines
}

// hasContent reports whether anything non-blank has been written to the pane.
func (s *solPane) hasContent() bool {
	if len(s.history) > 0 {
		return true
	}
	for _, row := range s.grid {
		for _, ch := range row {
			if ch != ' ' {
				return true
			}
		}
	}
	return false
}

func rowToString(row []rune) string {
	end := len(row)
	for end > 0 && row[end-1] == ' ' {
		end--
	}
	return string(row[:end])
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

/* ---------------- COMMANDS ---------------- */

// startSOLPane forks ipmitool sol activate inside a pty.
func startSOLPane(host, user, pass string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS == "windows" {
			return solReadMsg{Err: fmt.Errorf("built-in SOL pane not supported on Windows")}
		}
		// Best-effort: clear any SOL payload the BMC still thinks is active
		// from a previous session that didn't end cleanly (app killed,
		// terminal closed, network drop — none of which give the app a
		// chance to run its own F10/quit deactivate logic). Most BMCs allow
		// only one SOL session at a time, so a stuck slot makes every
		// subsequent "sol activate" silently fail even though the chassis
		// itself is powered on and reachable. Error is ignored: if nothing
		// was actually active this just fails fast, and a real connectivity
		// problem will surface again on the activate attempt below.
		_ = ipmi.SOLDeactivate(host, user, pass)

		p, err := startSolProcess(ipmi.SOLCmd(host, user, pass), cols, rows)
		if err != nil {
			return solReadMsg{Err: err}
		}
		return solPaneReadyMsg{pane: p}
	}
}

// shutdownSOL tears a session down off the UI goroutine (see
// solPane.shutdown) and, if ipmitool had to be signalled, sends an explicit
// "sol deactivate" so the BMC frees its SOL slot.
func shutdownSOL(p *solPane, host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		var err error
		if p.shutdown() && host != "" {
			err = ipmi.SOLDeactivate(host, user, pass)
		}
		return solClosedMsg{host: host, err: err}
	}
}

// listenSOL blocks on one pty read and returns the result as a tea.Msg.
// gen identifies the session so stale messages are discarded on reconnect.
// EOF/EIO means ipmitool has exited (it holds the only slave fds), so wait
// briefly for the reaper to record its exit status.
func listenSOL(p *solPane, gen int) tea.Cmd {
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			return solReadMsg{Data: data, Gen: gen}
		}
		if err != nil {
			var exitErr error
			if p.waitExit(solKillWait) {
				exitErr = p.exitErr
			}
			return solDoneMsg{gen: gen, exitErr: exitErr}
		}
		return solReadMsg{Gen: gen} // zero-byte read — retry
	}
}

/* ---------------- KEY MAPPING ---------------- */

// keyToBytes converts a bubbletea KeyMsg to the byte sequence the pty expects.
// Returns nil for keys that should not be forwarded (e.g. the disconnect key).
func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte{'\r'}
	case "backspace", "ctrl+backspace", "ctrl+h":
		return nil // handled in updateSOL: ESC[D + Ctrl+D (cursor-left + delete-forward)
	case "tab":
		return []byte{'\t'}
	case "up":
		return []byte{'\x1b', '[', 'A'}
	case "down":
		return []byte{'\x1b', '[', 'B'}
	case "right":
		return []byte{'\x1b', '[', 'C'}
	case "left":
		return []byte{'\x1b', '[', 'D'}
	case "ctrl+a":
		return []byte{'\x01'}
	case "ctrl+b":
		return []byte{'\x02'}
	case "ctrl+c":
		return []byte{'\x03'}
	case "ctrl+d":
		return []byte{'\x04'}
	case "ctrl+e":
		return []byte{'\x05'}
	case "ctrl+f":
		return []byte{'\x06'}
	case "ctrl+g":
		return []byte{'\x07'}
	case "ctrl+k":
		return []byte{'\x0b'}
	case "ctrl+l":
		return []byte{'\x0c'}
	case "ctrl+n":
		return []byte{'\x0e'}
	case "ctrl+o":
		return []byte{'\x0f'}
	case "ctrl+p":
		return []byte{'\x10'}
	case "ctrl+q":
		return []byte{'\x11'}
	case "ctrl+r":
		return []byte{'\x12'}
	case "ctrl+s":
		return []byte{'\x13'}
	case "ctrl+t":
		return []byte{'\x14'}
	case "ctrl+u":
		return []byte{'\x15'}
	case "ctrl+v":
		return []byte{'\x16'}
	case "ctrl+w":
		return []byte{'\x17'}
	case "ctrl+x":
		return []byte{'\x18'}
	case "ctrl+y":
		return []byte{'\x19'}
	case "ctrl+z":
		return []byte{'\x1a'}
	case "esc":
		return []byte{'\x1b'}
	case "home":
		return []byte{'\x1b', '[', 'H'}
	case "end":
		return []byte{'\x1b', '[', 'F'}
	case "delete":
		return []byte{'\x1b', '[', '3', '~'}
	case "f1":
		return []byte{'\x1b', 'O', 'P'}
	case "f2":
		return []byte{'\x1b', 'O', 'Q'}
	case "f3":
		return []byte{'\x1b', 'O', 'R'}
	case "f4":
		return []byte{'\x1b', 'O', 'S'}
	case "f5":
		return []byte{'\x1b', '[', '1', '5', '~'}
	case "f6":
		return []byte{'\x1b', '[', '1', '7', '~'}
	case "f7":
		return []byte{'\x1b', '[', '1', '8', '~'}
	case "f8":
		return []byte{'\x1b', '[', '1', '9', '~'}
	case "f9":
		return []byte{'\x1b', '[', '2', '0', '~'}
	case "f10":
		return nil // disconnect key — never forward
	case "f11":
		return []byte{'\x1b', '[', '2', '3', '~'}
	case "f12":
		return []byte{'\x1b', '[', '2', '4', '~'}
	case "space":
		return []byte{' '}
	}
	s := msg.String()
	if s != "" && !containsPlus(s) {
		return []byte(s)
	}
	return nil
}

func containsPlus(s string) bool {
	for _, r := range s {
		if r == '+' {
			return true
		}
	}
	return false
}
