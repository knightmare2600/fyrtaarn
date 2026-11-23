// Package amiweb talks to the web RPC interface of pre-Redfish AMI MegaRAC
// SP BMCs (GoAhead-Webs, /rpc/*.asp), as found on Quanta S2RQ/S2RS boards.
// It covers what fyrtaarn needs for virtual media: login, the KVM token the
// IUSB media ports authenticate with, and LMedia (images stored on the BMC).
//
// Every call goes over internal/legacytls, since these BMCs speak TLS 1.0
// with RC4-MD5 at best. BMC accounts are the IPMI user database, so the
// credentials fyrtaarn already holds for IPMI log in here too.
package amiweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/legacytls"
)

// ErrNotAMI means the BMC's web server is not an AMI MegaRAC SP RPC
// interface (no /rpc/WEBSES/create.asp, or an answer it does not recognise).
var ErrNotAMI = errors.New("not an AMI MegaRAC SP web interface")

// Client is a logged-in web session.
type Client struct {
	host   string
	cookie string
	http   *http.Client
}

func newHTTP(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: legacytls.Transport()}
}

// Login opens a web session with the BMC's user database.
func Login(host, user, pass string) (*Client, error) {
	c := &Client{host: host, http: newHTTP(60 * time.Second)}
	body, err := c.post("/rpc/WEBSES/create.asp", url.Values{
		"WEBVAR_USERNAME": {user},
		"WEBVAR_PASSWORD": {pass},
	})
	if err != nil {
		return nil, err
	}
	recs, status, err := parseRPC(body)
	if err != nil {
		return nil, ErrNotAMI
	}
	if status != 0 || len(recs) == 0 || recs[0]["SESSION_COOKIE"] == "" {
		return nil, fmt.Errorf("web login refused for %s (status %d): check username and password", user, status)
	}
	// A wrong password gets HAPI_STATUS -6 and the cookie
	// "Failure_Login_IPMI_Then_LDAP_then_Active_Directory_Radius" (Quanta,
	// FW 7.54); the prefix check covers firmware that answers status 0.
	if strings.HasPrefix(recs[0]["SESSION_COOKIE"], "Failure") {
		return nil, fmt.Errorf("web login refused for %s: check username and password", user)
	}
	c.cookie = recs[0]["SESSION_COOKIE"]
	return c, nil
}

// Logout ends the web session; errors are ignored, as the session expires
// on its own anyway.
func (c *Client) Logout() {
	_, _ = c.get("/rpc/WEBSES/logout.asp")
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	if c.cookie != "" {
		req.Header.Set("Cookie", "SessionCookie="+c.cookie+"; Language=EN")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	// GoAhead sometimes sends a Content-Length larger than the body it then
	// writes (seen on the JViewer launch file); keep what arrived.
	if err != nil && !(errors.Is(err, io.ErrUnexpectedEOF) && len(b) > 0) {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotAMI
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %s", req.URL.Path, resp.Status)
	}
	return b, nil
}

func (c *Client) get(p string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, "https://"+c.host+p, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) post(p string, form url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, "https://"+c.host+p, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req)
}

// rpc runs a .asp call and returns its records, failing on a non-zero
// HAPI_STATUS.
func (c *Client) rpc(p string, form url.Values) ([]map[string]string, error) {
	var b []byte
	var err error
	if form == nil {
		b, err = c.get(p)
	} else {
		b, err = c.post(p, form)
	}
	if err != nil {
		return nil, err
	}
	recs, status, err := parseRPC(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if status != 0 {
		return recs, fmt.Errorf("%s: BMC returned status %d", p, status)
	}
	return recs, nil
}

var (
	recordRE = regexp.MustCompile(`\{([^{}]*)\}`)
	fieldRE  = regexp.MustCompile(`'(\w+)'\s*:\s*(?:'([^']*)'|(-?\d+))`)
	statusRE = regexp.MustCompile(`HAPI_STATUS\s*:\s*(-?\d+)`)
)

// parseRPC reads the BMC's JavaScript-ish answer:
//
//	WEBVAR_JSONVAR_X = { WEBVAR_STRUCTNAME_X : [ { 'A' : 1,'B' : 'x' }, {} ], HAPI_STATUS:0 };
//
// into one map per non-empty record, all values as strings.
func parseRPC(b []byte) ([]map[string]string, int, error) {
	m := statusRE.FindSubmatch(b)
	if m == nil {
		return nil, 0, errors.New("no HAPI_STATUS in reply")
	}
	status, _ := strconv.Atoi(string(m[1]))
	var recs []map[string]string
	for _, r := range recordRE.FindAllSubmatch(b, -1) {
		fs := fieldRE.FindAllSubmatch(r[1], -1)
		if len(fs) == 0 {
			continue
		}
		rec := map[string]string{}
		for _, f := range fs {
			v := string(f[2])
			if len(f[3]) > 0 {
				v = string(f[3])
			}
			rec[string(f[1])] = v
		}
		recs = append(recs, rec)
	}
	return recs, status, nil
}

// MediaPorts is what the JViewer launch file says about virtual media.
type MediaPorts struct {
	Token  string // authenticates the IUSB media connections
	CDPort int
	FDPort int  // floppy; 0 if the launch file does not give one
	HDPort int  // hard disk / USB key; 0 if not given
	Secure bool // media ports want TLS (not supported by iusb yet)
}

var argRE = regexp.MustCompile(`<argument>\s*(-[a-z]+)\s*</argument>\s*<argument>\s*([^<]*?)\s*</argument>`)

// MediaPorts fetches the JViewer launch file the way the BMC's own page
// does. EXTRNIP must be present: without it this firmware formats a NULL
// host into the file and its web server crashes, staying down until the BMC
// is reset (seen on the Quanta, FW 7.54).
func (c *Client) MediaPorts() (MediaPorts, error) {
	ip := c.host
	if h, _, err := net.SplitHostPort(c.host); err == nil {
		ip = h
	}
	b, err := c.get("/Java/jviewer.jnlp?EXTRNIP=" + url.QueryEscape(ip) + "&JNLPSTR=JViewer")
	if err != nil {
		return MediaPorts{}, err
	}
	args := map[string]string{}
	for _, m := range argRE.FindAllSubmatch(b, -1) {
		args[string(m[1])] = string(m[2])
	}
	mp := MediaPorts{Token: args["-kvmtoken"], Secure: args["-vmsecure"] == "1"}
	mp.CDPort, _ = strconv.Atoi(args["-cdport"])
	mp.FDPort, _ = strconv.Atoi(args["-fdport"])
	mp.HDPort, _ = strconv.Atoi(args["-hdport"])
	if mp.Token == "" || mp.CDPort == 0 {
		return mp, errors.New("JViewer launch file has no KVM token or CD port")
	}
	return mp, nil
}

// LMedia image types, as the BMC numbers them.
const (
	LMediaCD     = 1
	LMediaFloppy = 2
	LMediaHDD    = 4
)

// LMediaMaxBytes is the upload limit. It is not reported by any RPC: the
// BMC's own upload page hard-codes it into the form ("8GB maximum size ...
// now 16MB"), and the BMC stores images in its own RAM or flash.
const LMediaMaxBytes = 16 << 20

const lmediaDir = "/usr/local/tmplmedia/"

// LMediaSlot is one image slot on the BMC.
type LMediaSlot struct {
	Type   int
	Name   string // full path on the BMC, empty when the slot is free
	Exists bool
}

// LMediaEnabled reports whether LMedia is switched on.
func (c *Client) LMediaEnabled() (bool, error) {
	recs, err := c.rpc("/rpc/getremotesession.asp", nil)
	if err != nil {
		return false, err
	}
	if len(recs) == 0 {
		return false, errors.New("getremotesession.asp returned no record")
	}
	return recs[0]["LMEDIAENABLE"] == "1", nil
}

// SetLMediaEnabled switches LMedia on or off. The BMC's page warns this
// closes every open virtual-media redirection.
func (c *Client) SetLMediaEnabled(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	_, err := c.rpc("/rpc/setlmediacfg.asp", url.Values{"ENABLE": {v}})
	return err
}

// LMediaSlots lists the image slots.
func (c *Client) LMediaSlots() ([]LMediaSlot, error) {
	recs, err := c.rpc("/rpc/getlmediaimage.asp", nil)
	if err != nil {
		return nil, err
	}
	var out []LMediaSlot
	for _, r := range recs {
		t, err := strconv.Atoi(r["IMAGE_TYPE"])
		if err != nil {
			continue
		}
		out = append(out, LMediaSlot{Type: t, Name: r["IMAGE_NAME"], Exists: r["IMAGE_EXISTS"] != "0" && r["IMAGE_EXISTS"] != ""})
	}
	return out, nil
}

// UploadLMedia stores an image in the slot for imgType, replacing what is
// there. name is the file's base name: .iso for CD, .img otherwise, as the
// BMC's page insists. The BMC serves no other request while it receives the
// file.
func (c *Client) UploadLMedia(ctx context.Context, imgType int, name string, r io.Reader, size int64) error {
	if size > LMediaMaxBytes {
		return fmt.Errorf("image is %d MiB; this BMC stores at most %d MiB", size>>20, LMediaMaxBytes>>20)
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	want := ".img"
	if imgType == LMediaCD {
		want = ".iso"
	}
	if !strings.HasSuffix(strings.ToLower(name), want) {
		return fmt.Errorf("the BMC only accepts %s files for this slot", want)
	}
	slots, err := c.LMediaSlots()
	if err != nil {
		return err
	}
	oper := 0
	for _, s := range slots {
		if s.Type == imgType {
			oper = 1 // add
			if s.Exists {
				oper = 2 // replace
			}
		}
	}
	if oper == 0 {
		return fmt.Errorf("the BMC has no LMedia slot of type %d", imgType)
	}
	target := lmediaDir + name

	// The page names the file field "<path>?<limit>" and posts it to
	// /page/file_upload.html; GoAhead uses the name as the destination.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(target+"?"+strconv.Itoa(LMediaMaxBytes), name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, io.LimitReader(r, LMediaMaxBytes+1)); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+c.host+"/page/file_upload.html", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	up := &Client{host: c.host, cookie: c.cookie, http: newHTTP(15 * time.Minute)}
	if _, err := up.do(req); err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	_, err = c.rpc("/rpc/setlmediaimage.asp", url.Values{
		"IMAGE_OPER": {strconv.Itoa(oper)},
		"IMAGE_TYPE": {strconv.Itoa(imgType)},
		"IMAGE_NAME": {target},
	})
	if err != nil {
		// Seen on the Quanta (FW 7.54) for every image tried, including a
		// formatted FAT12 floppy and a valid ISO, and identically when the
		// browser's requests are replayed with curl: the upload is accepted
		// but registering it returns status -1.
		return fmt.Errorf("the BMC accepted the upload but would not register the image (%w); "+
			"LMedia is known not to work on some firmware", err)
	}
	return nil
}

// DeleteLMedia empties the slot for imgType.
func (c *Client) DeleteLMedia(imgType int) error {
	slots, err := c.LMediaSlots()
	if err != nil {
		return err
	}
	for _, s := range slots {
		if s.Type == imgType && s.Exists {
			_, err := c.rpc("/rpc/setlmediaimage.asp", url.Values{
				"IMAGE_OPER": {"3"},
				"IMAGE_TYPE": {strconv.Itoa(imgType)},
				"IMAGE_NAME": {s.Name},
			})
			return err
		}
	}
	return nil
}
