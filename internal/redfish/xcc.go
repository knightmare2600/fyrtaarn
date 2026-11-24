package redfish

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Lenovo XClarity Controller virtual media, from Lenovo's REST API
// reference (pubs.lenovo.com xcc-restapi and xcc2-restapi,
// insert_eject_virtual_media_patch, collection_virtual_media_get,
// virtual_media_properties_get):
//
//   - the collection is /redfish/v1/Systems/1/VirtualMedia on XCC2 and
//     /redfish/v1/Managers/1/VirtualMedia on XCC, with slots EXT1-EXT4,
//     RDOC1-2 and Remote1-4;
//   - only EXT slots take an image by URL ("Not support to insert/eject
//     Remote{N} media", "Not support to insert RDOC{N} media");
//   - insert is a PATCH of the slot: {"Image": url, "Inserted": true,
//     "WriteProtected": true}, with UserName/Password for shares that need
//     them; eject is a PATCH setting Image to null and Inserted to false;
//   - protocols: XCC2 HTTP, HTTPS, NFS or CIFS; XCC "HTTP or no credential
//     required NFS" by its PATCH page, though its properties page lists
//     HTTPS, NFS and CIFS. Images are not checked against either list here;
//     the XCC answers for its own firmware.
//
// The standard InsertMedia action is not documented for XCC, so the PATCH
// is used instead of the generic walker's action. Not yet checked on
// hardware: no XCC has been available.
type xccProvider struct {
	host, user, pass string
}

var errNoEXTSlot = errors.New("no XCC EXT virtual media slot found (only EXT1-EXT4 take an image by URL)")

// xccEXTSlots lists the EXT slots, XCC2's location first.
func (p *xccProvider) xccEXTSlots() ([]string, map[string]map[string]any, error) {
	c := httpClient()
	base := "https://" + p.host
	var lastErr error
	for _, col := range []string{"/redfish/v1/Systems/1/VirtualMedia", "/redfish/v1/Managers/1/VirtualMedia"} {
		res, err := fetchJSON(c, base+col, p.user, p.pass)
		if err != nil {
			lastErr = err
			continue
		}
		var slots []string
		state := map[string]map[string]any{}
		for _, m := range collectionPaths(res) {
			if !strings.HasPrefix(strings.ToUpper(path.Base(m)), "EXT") {
				continue
			}
			s, err := fetchJSON(c, base+m, p.user, p.pass)
			if err != nil {
				return nil, nil, err
			}
			slots = append(slots, m)
			state[m] = s
		}
		if len(slots) > 0 {
			return slots, state, nil
		}
	}
	if lastErr != nil {
		return nil, nil, fmt.Errorf("%w (last error: %v)", errNoEXTSlot, lastErr)
	}
	return nil, nil, errNoEXTSlot
}

func xccInserted(s map[string]any) bool {
	in, _ := s["Inserted"].(bool)
	img, _ := s["Image"].(string)
	return in || img != ""
}

// xccInsertBody is the PATCH body; credentials in the URL go to UserName
// and Password and are taken out of Image.
func xccInsertBody(image string) map[string]any {
	body := map[string]any{"Image": image, "Inserted": true, "WriteProtected": true}
	if u, err := url.Parse(image); err == nil && u.User != nil {
		body["UserName"] = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			body["Password"] = pw
		}
		u.User = nil
		body["Image"] = u.String()
	}
	return body
}

func (p *xccProvider) Insert(image string) error {
	slots, state, err := p.xccEXTSlots()
	if err != nil {
		return err
	}
	for _, s := range slots {
		if xccInserted(state[s]) {
			continue
		}
		r, err := doJSON(httpClient(), "PATCH", "https://"+p.host+s, p.user, p.pass, xccInsertBody(image))
		if err != nil {
			return err
		}
		if r.status >= 400 {
			return fmt.Errorf("XCC refused the image on %s: HTTP %d %s", path.Base(s), r.status, redfishMessage(r.body))
		}
		return nil
	}
	return fmt.Errorf("all %d XCC EXT slots hold an image: eject first", len(slots))
}

func (p *xccProvider) Eject() error {
	slots, state, err := p.xccEXTSlots()
	if err != nil {
		return err
	}
	for _, s := range slots {
		if !xccInserted(state[s]) {
			continue
		}
		r, err := doJSON(httpClient(), "PATCH", "https://"+p.host+s, p.user, p.pass, map[string]any{"Image": nil, "Inserted": false})
		if err != nil {
			return err
		}
		if r.status >= 400 {
			return fmt.Errorf("XCC refused to eject %s: HTTP %d %s", path.Base(s), r.status, redfishMessage(r.body))
		}
	}
	return nil
}
