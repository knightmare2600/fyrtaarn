package redfish

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

// Dell iDRAC virtual media.
//
// iDRAC9 (and iDRAC8 from 2.70.70.70, by Dell's release notes, though a
// Dell community thread reports it failing there) takes an HTTP(S) image
// through standard Redfish VirtualMedia: the generic walker, checked against
// an iDRAC9 4.20 capture.
//
// iDRAC 7 and 8 mount a network share instead, through Dell's OEM OS
// deployment service, as Dell's own Invoke-ConnectNetworkIsoImageREDFISH
// PowerShell module (v7.0) and iDRAC-Redfish-Scripting do:
//
//	POST <base>/Actions/DellOSDeploymentService.ConnectNetworkISOImage
//	     {"IPAddress", "ShareType": "NFS"|"CIFS", "ShareName", "ImageName",
//	      ["UserName", "Password"]}
//	  -> 202 with a job in Location, polled until TaskState "Completed"
//	     ("Exception" is failure);
//	POST <base>/Actions/DellOSDeploymentService.GetAttachStatus {}
//	  -> {"ISOAttachStatus": "Attached" | "NotAttached"}
//	POST <base>/Actions/DellOSDeploymentService.DisconnectNetworkISOImage {}
//
// where <base> is /redfish/v1/Dell/Systems/System.Embedded.1/DellOSDeploymentService
// in the PowerShell module and /redfish/v1/Systems/System.Embedded.1/Oem/Dell/DellOSDeploymentService
// in the Python scripts; whichever answers a GET is used. Dell's scripts
// state "Supported values are NFS and CIFS" and no minimum firmware.
//
// Checked read-only against an iDRAC9 4.20 (fy-capture.sh, 2026-11-24):
// the /redfish/v1/Dell/... prefix answers and the .../Oem/Dell/... one is a
// 404; the service lists each action with its target, and
// ConnectNetworkISOImage with "ShareType@Redfish.AllowableValues":
// ["CIFS", "NFS"]; GetAttachStatus answers {"ISOAttachStatus":
// "NotAttached", "DriversAttachStatus": ...}. So the targets the service
// lists are used, and the share type is checked against its list.
//
// So: an nfs:// or cifs:// (smb://) image goes to the OEM service; an
// http(s):// one to the generic walker. fyrtaarn's own local-file serving
// is HTTP, which iDRAC 7/8 cannot fetch: a local file needs a share there.
// Not yet checked on hardware: no iDRAC 7 or 8 has been available.
type idracProvider struct {
	host, user, pass string
}

var dellOSDBases = []string{
	"/redfish/v1/Dell/Systems/System.Embedded.1/DellOSDeploymentService",
	"/redfish/v1/Systems/System.Embedded.1/Oem/Dell/DellOSDeploymentService",
}

// IsShareURL reports whether image is an NFS or CIFS share URL.
func IsShareURL(image string) bool {
	u, err := url.Parse(image)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "nfs", "cifs", "smb":
		return true
	}
	return false
}

// dellShare turns a share URL into ConnectNetworkISOImage's body:
// nfs://host/export/dir/x.iso gives ShareName "/export/dir"; for CIFS the
// share name has no leading slash (cifs://u:p@host/share/dir/x.iso gives
// "share/dir"), as Dell's examples name CIFS shares. How iDRAC 7/8 take a
// subfolder in a CIFS share name is not documented in the sources read.
func dellShare(image string) (map[string]any, error) {
	u, err := url.Parse(image)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("not a share URL: %q", image)
	}
	file := path.Base(u.Path)
	dir := path.Dir(u.Path)
	if file == "" || file == "/" || file == "." || dir == "/" && !strings.EqualFold(u.Scheme, "nfs") {
		return nil, fmt.Errorf("share URL needs a share and a file name: %q", image)
	}
	body := map[string]any{"IPAddress": u.Hostname(), "ImageName": file}
	switch strings.ToLower(u.Scheme) {
	case "nfs":
		body["ShareType"], body["ShareName"] = "NFS", dir
	case "cifs", "smb":
		body["ShareType"], body["ShareName"] = "CIFS", strings.TrimPrefix(dir, "/")
	default:
		return nil, fmt.Errorf("iDRAC network ISO takes nfs:// or cifs://, not %q", u.Scheme)
	}
	if u.User != nil {
		body["UserName"] = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			body["Password"] = pw
		}
	}
	return body, nil
}

// dellOSD is the OS deployment service as the iDRAC describes it.
type dellOSD struct {
	base    string
	actions map[string]any // its Actions object
}

func (p *idracProvider) osdBase() (string, error) {
	o, err := p.osd()
	return o.base, err
}

func (p *idracProvider) osd() (dellOSD, error) {
	c := httpClient()
	for _, b := range dellOSDBases {
		if res, err := fetchJSON(c, "https://"+p.host+b, p.user, p.pass); err == nil {
			acts, _ := res["Actions"].(map[string]any)
			return dellOSD{base: b, actions: acts}, nil
		}
	}
	return dellOSD{}, errors.New("this iDRAC has no Dell OS deployment service over Redfish (needs a later iDRAC 7/8 firmware)")
}

// target is the action's URL as the service lists it, else built from base.
func (o dellOSD) target(name string) string {
	if a, ok := o.actions["#DellOSDeploymentService."+name].(map[string]any); ok {
		if t, ok := a["target"].(string); ok && t != "" {
			return t
		}
	}
	return o.base + "/Actions/DellOSDeploymentService." + name
}

// allows reports whether the action lists value among param's allowable
// values; true when it lists none.
func (o dellOSD) allows(name, param, value string) (bool, []string) {
	a, _ := o.actions["#DellOSDeploymentService."+name].(map[string]any)
	vals, ok := a[param+"@Redfish.AllowableValues"].([]any)
	if !ok {
		return true, nil
	}
	var list []string
	for _, v := range vals {
		if sv, ok := v.(string); ok {
			list = append(list, sv)
			if strings.EqualFold(sv, value) {
				return true, list
			}
		}
	}
	return false, list
}

// dellJobWait bounds the wait for the connect job; a variable for tests.
var dellJobWait = 5 * time.Minute

func (p *idracProvider) action(o dellOSD, name string, body any) (vendorResponse, error) {
	r, err := doJSON(httpClient(), "POST", "https://"+p.host+o.target(name), p.user, p.pass, body)
	if err != nil {
		return r, err
	}
	if r.status >= 400 {
		return r, fmt.Errorf("%s: HTTP %d %s", name, r.status, redfishMessage(r.body))
	}
	return r, nil
}

// waitJob follows a 202's Location until TaskState is Completed.
func (p *idracProvider) waitJob(loc string) error {
	if loc == "" {
		return nil // answered at once
	}
	if !strings.HasPrefix(loc, "http") {
		loc = "https://" + p.host + loc
	}
	deadline := time.Now().Add(dellJobWait)
	for {
		j, err := fetchJSON(httpClient(), loc, p.user, p.pass)
		if err == nil {
			switch str(j, "TaskState") {
			case "Completed":
				return nil
			case "Exception", "Killed", "Cancelled":
				return fmt.Errorf("iDRAC job ended %s: %s", str(j, "TaskState"), firstStr(j, "Message", "TaskStatus"))
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("iDRAC job not complete after %s", dellJobWait)
		}
		time.Sleep(3 * time.Second)
	}
}

func (p *idracProvider) attached(o dellOSD) (bool, error) {
	r, err := p.action(o, "GetAttachStatus", map[string]any{})
	if err != nil {
		return false, err
	}
	var s struct{ ISOAttachStatus string }
	_ = json.Unmarshal(r.body, &s)
	return s.ISOAttachStatus == "Attached", nil
}

func (p *idracProvider) Insert(image string) error {
	if !IsShareURL(image) {
		err := (&genericProvider{host: p.host, user: p.user, pass: p.pass}).Insert(image)
		if err != nil && strings.Contains(err.Error(), "could not locate Redfish VirtualMedia") {
			if _, oerr := p.osdBase(); oerr == nil {
				// No standard VirtualMedia, but the OEM network ISO service:
				// an iDRAC 7/8, which mounts NFS or CIFS shares only.
				return errors.New("this iDRAC mounts images from NFS or CIFS shares only: give nfs://server/export/dir/image.iso or cifs://user:password@server/share/image.iso (a local file is served over HTTP, which it cannot use)")
			}
		}
		return err
	}
	body, err := dellShare(image)
	if err != nil {
		return err
	}
	o, err := p.osd()
	if err != nil {
		return err
	}
	if ok, list := o.allows("ConnectNetworkISOImage", "ShareType", body["ShareType"].(string)); !ok {
		return fmt.Errorf("this iDRAC takes share types %s, not %s", strings.Join(list, ", "), body["ShareType"])
	}
	r, err := p.action(o, "ConnectNetworkISOImage", body)
	if err != nil {
		return err
	}
	if err := p.waitJob(r.header.Get("Location")); err != nil {
		return err
	}
	if ok, err := p.attached(o); err == nil && !ok {
		return errors.New("the iDRAC reports the ISO as not attached after the job completed")
	}
	return nil
}

// Eject disconnects a network ISO if the OEM service reports one, and
// ejects standard virtual media as the generic walker does.
func (p *idracProvider) Eject() error {
	var errs []error
	if o, err := p.osd(); err == nil {
		if ok, err := p.attached(o); err == nil && ok {
			if _, err := p.action(o, "DisconnectNetworkISOImage", map[string]any{}); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := (&genericProvider{host: p.host, user: p.user, pass: p.pass}).Eject(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 2 {
		return errors.Join(errs...)
	}
	if len(errs) == 1 && !strings.Contains(errs[0].Error(), "could not locate Redfish VirtualMedia") {
		return errs[0]
	}
	return nil
}
