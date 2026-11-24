package redfish

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// iLO virtual media.
//
// # iLO 4 (confirmed from live ProLiant ML310e Gen8 v2, FW 2.54)
//
// Redfish IS present but uses an older schema with HP-specific OEM extensions:
//
//   - Service root at /redfish/v1/ returns both "@odata.id" Members (standard)
//     and "links.Member[].href" (HP legacy) — collectionPaths() handles both.
//   - FirmwareVersion: "iLO 4 v2.54" (standard field populated correctly).
//   - Manager Status: only {"State": "Enabled"} — no Health key at root level.
//   - System fields present: Manufacturer, Model, SerialNumber, SKU, HostName,
//     BiosVersion, PowerState, ProcessorSummary, MemorySummary. String fields
//     may have trailing whitespace — str() trims these.
//   - MemorySummary.Status uses "HealthRollUp" (capital U) not "HealthRollup".
//   - VirtualMedia: two numeric slots under /redfish/v1/Managers/1/VirtualMedia/
//     — slot 1 has MediaTypes ["Floppy", "USBStick"]; slot 2 has MediaTypes
//     ["CD", "DVD"] and is the ISO mount target.
//   - VirtualMedia actions are NOT at the standard Actions map. They are under
//     Oem.Hp.Actions with HP-specific names, each holding a target URL:
//     #HpiLOVirtualMedia.InsertVirtualMedia and
//     #HpiLOVirtualMedia.EjectVirtualMedia.
//     Insert body: {"Image": "http://...iso"} — Intent and Signature are
//     optional; Inserted/WriteProtected are not accepted by the HP OEM endpoint.
//   - Auth: HTTP Basic Auth works. Session-token auth not required on iLO 4.
//
// The generic provider (virtualmedia.go) handles all of the above, as it
// does iLO 5 (standard actions, checked against a capture). No separate
// backend is needed for iLO 4 or 5.
//
// # iLO 3 (no Redfish): RIBCL, this file
//
// RIBCL 2.0 XML POSTed to /ribcl over HTTPS, as python-hpilo 4.4.2 does for
// iLO 3 and later (hpilo.py: HTTP_HEADER "POST /ribcl HTTP/1.1", XML_HEADER
// '<?xml version="1.0"?>', _root_element, _parse_message):
//
//	<?xml version="1.0"?>
//	<RIBCL VERSION="2.0"><LOGIN USER_LOGIN="u" PASSWORD="p">
//	  <RIB_INFO MODE="write"><INSERT_VIRTUAL_MEDIA DEVICE="CDROM" IMAGE_URL="http://..."/></RIB_INFO>
//	</LOGIN></RIBCL>
//
// then SET_VM_STATUS with VM_BOOT_OPTION value="CONNECT" to connect the
// inserted image (python-hpilo: "you will also need to use set_vm_status to
// connect the media"), and EJECT_VIRTUAL_MEDIA to eject. Each request is
// answered by one or more XML documents whose RESPONSE elements carry
// STATUS (hex; 0 is success) and MESSAGE. An HTTP 404 from /ribcl means
// iLO 2 or older, which speaks RIBCL only over a raw TLS stream; that is
// not built.
//
// The password goes into the PASSWORD attribute unescaped, as python-hpilo
// sends it (DoNotEscapeMe): iLO's RIBCL parser is not documented to decode
// XML entities there. A password containing a double quote cannot be sent
// this way and is refused before anything is sent.
//
// The RIBCL transport and replies were checked read-only against an iLO 4
// (2.54) and an iLO 5 (1.40), which still answer /ribcl (fy-capture.sh,
// 2026-11-24): HTTP 200, chunked, Content-Type text/xml; one reply holds
// five XML documents, each with a RESPONSE (STATUS="0x0000",
// MESSAGE='No error', attributes on separate lines), the last also holding
// the data, e.g. GET_VM_STATUS with VM_APPLET, DEVICE, BOOT_OPTION,
// WRITE_PROTECT, IMAGE_INSERTED and IMAGE_URL. Inserting and ejecting are
// not checked on hardware, and no iLO 3 has been available.
type ilo3Provider struct {
	host, user, pass string
}

// ribclDevice is the RIBCL DEVICE for an image: CDROM, or FLOPPY for disk
// and floppy images (iLO 3 offers no USB-key slot over RIBCL).
func ribclDevice(image string) string {
	if KindForImage(image) == MediaRemovable {
		return "FLOPPY"
	}
	return "CDROM"
}

// ribclDoc wraps one RIB_INFO command in a RIBCL document.
func ribclDoc(user, pass, mode, command string) ([]byte, error) {
	if strings.ContainsAny(pass, `"`) {
		return nil, errors.New("iLO 3 RIBCL cannot carry a password containing a double quote")
	}
	var u bytes.Buffer
	_ = xml.EscapeText(&u, []byte(user))
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\"?>\r\n")
	fmt.Fprintf(&b, `<RIBCL VERSION="2.0"><LOGIN USER_LOGIN="%s" PASSWORD="%s"><RIB_INFO MODE="%s">%s</RIB_INFO></LOGIN></RIBCL>`,
		strings.ReplaceAll(u.String(), `"`, "&quot;"), pass, mode, command)
	b.WriteString("\r\n")
	return b.Bytes(), nil
}

func xmlAttr(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return strings.ReplaceAll(b.String(), `"`, "&quot;")
}

// RIBCLError is a non-zero RESPONSE STATUS.
type RIBCLError struct {
	Status  int
	Message string
}

func (e *RIBCLError) Error() string {
	return fmt.Sprintf("iLO answered 0x%04x: %s", e.Status, e.Message)
}

// ribclElement returns the attributes of the first element named name in a
// reply, or nil.
func ribclElement(reply []byte, name string) map[string]string {
	for _, d := range bytes.Split(reply, []byte("<?xml")) {
		dec := xml.NewDecoder(bytes.NewReader(append([]byte("<?xml"), d...)))
		dec.Strict = false
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == name {
				out := map[string]string{}
				for _, a := range se.Attr {
					out[a.Name.Local] = a.Value
				}
				return out
			}
		}
	}
	return nil
}

// parseRIBCL checks every RESPONSE in a reply, which may be several XML
// documents one after another, and returns the first error.
func parseRIBCL(reply []byte) error {
	docs := bytes.Split(reply, []byte("<?xml"))
	seen := false
	for _, d := range docs {
		if len(bytes.TrimSpace(d)) == 0 {
			continue
		}
		dec := xml.NewDecoder(bytes.NewReader(append([]byte("<?xml"), d...)))
		dec.Strict = false
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if !ok || se.Name.Local != "RESPONSE" {
				continue
			}
			seen = true
			var status, msg string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "STATUS":
					status = a.Value
				case "MESSAGE":
					msg = a.Value
				}
			}
			n, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(status), "0x"), 16, 32)
			if err != nil {
				return fmt.Errorf("iLO reply has an unreadable STATUS %q", status)
			}
			if n != 0 {
				return &RIBCLError{Status: int(n), Message: msg}
			}
		}
	}
	if !seen {
		return errors.New("iLO reply has no RESPONSE: maybe not an iLO")
	}
	return nil
}

func (p *ilo3Provider) send(mode, command string) error {
	_, err := p.request(mode, command)
	return err
}

// request sends one command and returns the reply once checked.
func (p *ilo3Provider) request(mode, command string) ([]byte, error) {
	doc, err := ribclDoc(p.user, p.pass, mode, command)
	if err != nil {
		return nil, err
	}
	reply, err := postXML(httpClient(), "https://"+p.host+"/ribcl", doc)
	var se *httpStatusError
	if errors.As(err, &se) && se.code == http.StatusNotFound {
		return nil, errors.New("no RIBCL over HTTP at /ribcl: iLO 2 or older, whose raw RIBCL is not supported")
	}
	if err != nil {
		return nil, err
	}
	return reply, parseRIBCL(reply)
}

// vmStatus reads GET_VM_STATUS for dev; nil if the reply has none.
func (p *ilo3Provider) vmStatus(dev string) (map[string]string, error) {
	reply, err := p.request("read", fmt.Sprintf(`<GET_VM_STATUS DEVICE="%s"/>`, dev))
	if err != nil {
		return nil, err
	}
	return ribclElement(reply, "GET_VM_STATUS"), nil
}

// Insert inserts the image and connects it, without changing the boot order.
func (p *ilo3Provider) Insert(image string) error {
	dev := ribclDevice(image)
	if err := p.send("write", fmt.Sprintf(`<INSERT_VIRTUAL_MEDIA DEVICE="%s" IMAGE_URL="%s"/>`, dev, xmlAttr(image))); err != nil {
		return fmt.Errorf("inserting the image: %w", err)
	}
	connect := fmt.Sprintf(`<SET_VM_STATUS DEVICE="%s"><VM_BOOT_OPTION value="CONNECT"/><VM_WRITE_PROTECT value="YES"/></SET_VM_STATUS>`, dev)
	if err := p.send("write", connect); err != nil {
		return fmt.Errorf("connecting the image: %w", err)
	}
	// Read back: an image the iLO took but did not attach is said so.
	if st, err := p.vmStatus(dev); err == nil && st != nil && st["IMAGE_INSERTED"] != "YES" {
		return fmt.Errorf("the iLO accepted the image but reports it not inserted (VM_APPLET %s)", st["VM_APPLET"])
	}
	return nil
}

// Eject ejects each device whose GET_VM_STATUS reports an image inserted.
// If the status cannot be read, both devices are asked to eject, and a
// refusal counts as "nothing there" as long as one of them ejected.
func (p *ilo3Provider) Eject() error {
	devs := []string{"CDROM", "FLOPPY"}
	var inserted []string
	known := true
	for _, dev := range devs {
		st, err := p.vmStatus(dev)
		var re *RIBCLError
		if err != nil && !errors.As(err, &re) {
			return err // transport or login failure: say so
		}
		if err != nil || st == nil {
			known = false
			break
		}
		if st["IMAGE_INSERTED"] == "YES" {
			inserted = append(inserted, dev)
		}
	}
	if known {
		for _, dev := range inserted {
			if err := p.send("write", fmt.Sprintf(`<EJECT_VIRTUAL_MEDIA DEVICE="%s"/>`, dev)); err != nil {
				return fmt.Errorf("ejecting %s: %w", dev, err)
			}
		}
		return nil // nothing inserted is not an error
	}
	var errs []error
	for _, dev := range devs {
		err := p.send("write", fmt.Sprintf(`<EJECT_VIRTUAL_MEDIA DEVICE="%s"/>`, dev))
		var re *RIBCLError
		if err != nil && !errors.As(err, &re) {
			return err
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == len(devs) {
		return fmt.Errorf("nothing ejected: %v", errors.Join(errs...))
	}
	return nil
}
