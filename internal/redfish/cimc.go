package redfish

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// Cisco CIMC (standalone C-Series IMC) virtual media, over the IMC XML API
// as Cisco's imcsdk drives it (imcsdk/apis/server/vmedia.py,
// mometa/comm/CommVMediaMap.py, imcsession.py, imcmethodfactory.py):
//
//   - every request is an XML document POSTed to https://<bmc>/nuova;
//   - aaaLogin (inName, inPassword) returns a session cookie, which later
//     requests carry as a "cookie" attribute on their root element;
//     aaaLogout (inCookie) ends the session;
//   - a mapping is a commVMediaMap object, DN
//     sys/svc-ext/vmedia-svc/vmmap-<volumeName>, created with configConfMo,
//     with map ("www" for HTTP(S), "nfs", "cifs"), remoteShare (the URL up
//     to and including the last "/"), remoteFile (the file name) and
//     mountOptions ("noauto" for HTTP, as imcsdk sets); its mappingStatus
//     reads "In Progress" then "OK", or starts with "ERROR";
//   - volumeName may hold [-.:_a-zA-Z0-9], at most 47 characters.
//
// The serialised XML was not in the files read: element and attribute names
// follow the UCS/IMC XML API conventions imcsdk generates from (aaaLogin,
// outCookie, configConfMo with inConfig, configResolveDn with outConfig,
// configResolveChildren with inDn, status="created"/"removed", errorCode
// and errorDescr on failure). Not yet checked on hardware: no CIMC has
// been available.
type cimcProvider struct {
	host, user, pass string
}

const cimcVMediaDN = "sys/svc-ext/vmedia-svc"

var cimcVolumeBad = regexp.MustCompile(`[^-.:_a-zA-Z0-9]`)

// cimcVolumeName makes a valid volumeName from the image's file name.
func cimcVolumeName(file string) string {
	v := cimcVolumeBad.ReplaceAllString(file, "_")
	if len(v) > 47 {
		v = v[:47]
	}
	if v == "" {
		v = "fyrtaarn"
	}
	return v
}

// cimcMapping splits an image URL into the mapping's fields.
func cimcMapping(image string) (mapType, share, file, opts string, err error) {
	u, err := url.Parse(image)
	if err != nil || u.Host == "" {
		return "", "", "", "", fmt.Errorf("not a URL: %q", image)
	}
	file = path.Base(u.Path)
	if file == "" || file == "/" || file == "." {
		return "", "", "", "", fmt.Errorf("no file name in %q", image)
	}
	dir := path.Dir(u.Path)
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		share = u.Scheme + "://" + u.Host + dir
		return "www", share, file, "noauto", nil
	case "nfs":
		return "nfs", u.Hostname() + ":" + strings.TrimSuffix(dir, "/"), file, "", nil
	case "cifs", "smb":
		return "cifs", "//" + u.Hostname() + strings.TrimSuffix(dir, "/"), file, "", nil
	}
	return "", "", "", "", fmt.Errorf("CIMC maps http, https, nfs or cifs images, not %q", u.Scheme)
}

// cimcReply is the attributes of a reply's root element and of every
// commVMediaMap inside it.
type cimcReply struct {
	root map[string]string
	maps []map[string]string
}

func parseCIMC(b []byte) (cimcReply, error) {
	r := cimcReply{}
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, a := range se.Attr {
			attrs[a.Name.Local] = a.Value
		}
		if r.root == nil {
			r.root = attrs
		}
		if se.Name.Local == "commVMediaMap" {
			r.maps = append(r.maps, attrs)
		}
	}
	if r.root == nil {
		return r, errors.New("CIMC reply is not XML")
	}
	if code := r.root["errorCode"]; code != "" && code != "0" {
		return r, fmt.Errorf("CIMC error %s: %s", code, r.root["errorDescr"])
	}
	return r, nil
}

func (p *cimcProvider) call(doc string) (cimcReply, error) {
	b, err := postXML(httpClient(), "https://"+p.host+"/nuova", []byte(doc))
	if err != nil {
		return cimcReply{}, err
	}
	return parseCIMC(b)
}

func (p *cimcProvider) login() (string, error) {
	r, err := p.call(fmt.Sprintf(`<aaaLogin inName="%s" inPassword="%s"/>`, xmlAttr(p.user), xmlAttr(p.pass)))
	if err != nil {
		return "", fmt.Errorf("CIMC login: %w", err)
	}
	if r.root["outCookie"] == "" {
		return "", errors.New("CIMC login returned no session cookie")
	}
	return r.root["outCookie"], nil
}

func (p *cimcProvider) logout(cookie string) {
	_, _ = p.call(fmt.Sprintf(`<aaaLogout cookie="%s" inCookie="%s"/>`, xmlAttr(cookie), xmlAttr(cookie)))
}

func cimcMapDoc(cookie, dn, status string, attrs map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<configConfMo cookie="%s" dn="%s" inHierarchical="false"><inConfig><commVMediaMap dn="%s"`,
		xmlAttr(cookie), xmlAttr(dn), xmlAttr(dn))
	for _, k := range []string{"volumeName", "map", "remoteShare", "remoteFile", "mountOptions"} {
		if v, ok := attrs[k]; ok && v != "" {
			fmt.Fprintf(&b, ` %s="%s"`, k, xmlAttr(v))
		}
	}
	fmt.Fprintf(&b, ` status="%s"/></inConfig></configConfMo>`, status)
	return b.String()
}

// cimcMapWait and cimcMapPoll bound the wait for a mapping (imcsdk: every
// 10 s, 60 s at most); variables so tests can shorten them.
var (
	cimcMapWait = 60 * time.Second
	cimcMapPoll = 2 * time.Second
)

// Insert maps the image and waits for the CIMC to report it mounted.
func (p *cimcProvider) Insert(image string) error {
	mapType, share, file, opts, err := cimcMapping(image)
	if err != nil {
		return err
	}
	cookie, err := p.login()
	if err != nil {
		return err
	}
	defer p.logout(cookie)
	vol := cimcVolumeName(file)
	dn := cimcVMediaDN + "/vmmap-" + vol
	_, err = p.call(cimcMapDoc(cookie, dn, "created", map[string]string{
		"volumeName": vol, "map": mapType, "remoteShare": share, "remoteFile": file, "mountOptions": opts,
	}))
	if err != nil {
		return fmt.Errorf("mapping the image: %w", err)
	}
	deadline := time.Now().Add(cimcMapWait)
	for {
		r, err := p.call(fmt.Sprintf(`<configResolveDn cookie="%s" dn="%s" inHierarchical="false"/>`, xmlAttr(cookie), xmlAttr(dn)))
		if err != nil {
			return fmt.Errorf("reading the mapping: %w", err)
		}
		st := ""
		if len(r.maps) > 0 {
			st = r.maps[0]["mappingStatus"]
		}
		switch {
		case st == "OK":
			return nil
		case strings.HasPrefix(strings.ToUpper(st), "ERROR"):
			return fmt.Errorf("CIMC could not mount the image: %s", st)
		case time.Now().After(deadline):
			return fmt.Errorf("CIMC still reports %q after %s", st, cimcMapWait)
		}
		time.Sleep(cimcMapPoll)
	}
}

// Eject removes every mapping, as the generic Redfish eject ejects every
// inserted slot.
func (p *cimcProvider) Eject() error {
	cookie, err := p.login()
	if err != nil {
		return err
	}
	defer p.logout(cookie)
	r, err := p.call(fmt.Sprintf(`<configResolveChildren cookie="%s" classId="commVMediaMap" inDn="%s" inHierarchical="false"/>`, xmlAttr(cookie), cimcVMediaDN))
	if err != nil {
		return fmt.Errorf("listing mappings: %w", err)
	}
	for _, m := range r.maps {
		dn := m["dn"]
		if dn == "" && m["volumeName"] != "" {
			dn = cimcVMediaDN + "/vmmap-" + m["volumeName"]
		}
		if dn == "" {
			continue
		}
		if _, err := p.call(cimcMapDoc(cookie, dn, "removed", nil)); err != nil {
			return fmt.Errorf("removing %s: %w", dn, err)
		}
	}
	return nil
}
