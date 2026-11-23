package tui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/amiweb"
	"github.com/knightmare2600/fyrtaarn/internal/iusb"
)

// Virtual media for BMCs without Redfish: AMI MegaRAC SP network media
// redirection (iusb) and, as a fallback, LMedia uploads (amiweb).

// amiMediaMsg reports the outcome of attaching an image.
type amiMediaMsg struct {
	Host string
	Path string
	Stop func()
	Done <-chan error // receives once, when the redirection ends
	Err  error
}

// amiMediaEndedMsg reports that an attached image was detached.
type amiMediaEndedMsg struct {
	Host string
	Err  error
}

// lmediaMsg reports the outcome of an LMedia upload.
type lmediaMsg struct {
	Host, Name string
	Err        error
}

// expandHome resolves a leading ~/ the way the Redfish local-ISO path does.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// notAMIError explains a BMC that has neither Redfish nor AMI's interface.
func notAMIError(err error) error {
	if errors.Is(err, amiweb.ErrNotAMI) {
		return errors.New("this BMC has no Redfish and no AMI MegaRAC web interface; virtual media is not supported on it")
	}
	return err
}

// startAMIMedia logs in to the BMC's web interface, fetches the media token
// and attaches path as the host's virtual CD-ROM. The image is read on
// demand for as long as the redirection runs; the web session stays open
// alongside it and is closed when it ends.
func startAMIMedia(host, user, pass, path string) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg { return amiMediaMsg{Host: host, Path: path, Err: err} }
		f, err := os.Open(path)
		if err != nil {
			return fail(err)
		}
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			_ = f.Close()
			return fail(fmt.Errorf("%s is not a file", path))
		}
		web, err := amiweb.Login(host, user, pass)
		if err != nil {
			_ = f.Close()
			return fail(notAMIError(err))
		}
		mp, err := web.MediaPorts()
		if err == nil && mp.Secure {
			err = errors.New("the BMC requires encrypted media redirection, which fyrtaarn does not support yet")
		}
		if err != nil {
			web.Logout()
			_ = f.Close()
			return fail(err)
		}
		s, err := iusb.Dial(net.JoinHostPort(host, strconv.Itoa(mp.CDPort)), mp.Token, 0)
		if err != nil {
			web.Logout()
			_ = f.Close()
			return fail(err)
		}
		done := make(chan error, 1)
		go func() {
			err := s.ServeCD(f, st.Size(), nil)
			_ = f.Close()
			web.Logout()
			done <- err
		}()
		return amiMediaMsg{Host: host, Path: path, Stop: func() { _ = s.Close() }, Done: done}
	}
}

// waitAMIMedia turns the end of a redirection into a message.
func waitAMIMedia(host string, done <-chan error) tea.Cmd {
	return func() tea.Msg {
		return amiMediaEndedMsg{Host: host, Err: <-done}
	}
}

// lmediaType picks the BMC slot for a file: .iso is a CD; .img is a floppy
// up to 2.88 MB, a hard disk above that.
func lmediaType(path string, size int64) (int, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".iso":
		return amiweb.LMediaCD, nil
	case ".img":
		if size <= 2949120 {
			return amiweb.LMediaFloppy, nil
		}
		return amiweb.LMediaHDD, nil
	}
	return 0, errors.New("the BMC only stores .iso (CD) and .img (floppy or hard disk) files")
}

// uploadLMedia stores path on the BMC itself, switching LMedia on first if
// it is off (which closes any open media redirection, as the warning said).
func uploadLMedia(host, user, pass, path string) tea.Cmd {
	return func() tea.Msg {
		name := filepath.Base(path)
		fail := func(err error) tea.Msg { return lmediaMsg{Host: host, Name: name, Err: err} }
		f, err := os.Open(path)
		if err != nil {
			return fail(err)
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return fail(err)
		}
		if st.Size() > amiweb.LMediaMaxBytes {
			return fail(fmt.Errorf("%s is %d MiB; this BMC stores at most %d MiB",
				name, st.Size()>>20, amiweb.LMediaMaxBytes>>20))
		}
		typ, err := lmediaType(path, st.Size())
		if err != nil {
			return fail(err)
		}
		web, err := amiweb.Login(host, user, pass)
		if err != nil {
			return fail(notAMIError(err))
		}
		defer web.Logout()
		on, err := web.LMediaEnabled()
		if err == nil && !on {
			err = web.SetLMediaEnabled(true)
		}
		if err != nil {
			return fail(err)
		}
		return lmediaMsg{Host: host, Name: name,
			Err: web.UploadLMedia(context.Background(), typ, name, f, st.Size())}
	}
}
