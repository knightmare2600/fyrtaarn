package redfish

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Each fake BMC below answers as its vendor's documentation says (sources
// in ilo.go, cimc.go, xcc.go, drac.go). The tests check that fyrtaarn
// speaks those formats; they cannot show that real hardware agrees.

type recorder struct {
	mu   sync.Mutex
	reqs []string // "METHOD path body"
}

func (r *recorder) add(req *http.Request) string {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req.Method+" "+req.URL.Path+" "+string(b))
	return string(b)
}

func (r *recorder) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.reqs, "\n")
}

func tlsHost(t *testing.T, h http.HandlerFunc) string {
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

// --- iLO 3: RIBCL ---

// ilo3Reply is a reply in the shape an iLO 4 (2.54) and iLO 5 (1.40) sent
// (fy-capture.sh, 2026-11-24): five XML documents, CRLF after each XML
// declaration, attributes on their own lines, MESSAGE single-quoted, the
// last document also holding the data.
func ilo3Reply(status, msg, data string) string {
	doc := func(st, m, extra string) string {
		return "<?xml version=\"1.0\"?>\r\n<RIBCL VERSION=\"2.23\">\n<RESPONSE\n    STATUS=\"" + st + "\"\n    MESSAGE='" + m + "'\n     />\n" + extra + "</RIBCL>\n"
	}
	ok := doc("0x0000", "No error", "")
	return ok + ok + ok + ok + doc(status, msg, data)
}

func vmStatusXML(dev string, inserted bool, url string) string {
	in, applet := "NO", "DISCONNECTED"
	if inserted {
		in, applet = "YES", "CONNECTED"
	}
	return "<GET_VM_STATUS\n    VM_APPLET=\"" + applet + "\"\n    DEVICE=\"" + dev + "\"\n    BOOT_OPTION=\"NO_BOOT\"\n    WRITE_PROTECT=\"YES\"\n    IMAGE_INSERTED=\"" + in + "\"\n    IMAGE_URL=\"" + url + "\"\n    />\n"
}

// fakeILO keeps per-device state; attach false makes it accept an image
// without attaching it.
func fakeILO(t *testing.T, attach bool) (string, *recorder, map[string]string) {
	rec := &recorder{}
	state := map[string]string{} // device -> image URL
	host := tlsHost(t, func(w http.ResponseWriter, r *http.Request) {
		body := rec.add(r)
		if r.URL.Path != "/ribcl" {
			http.NotFound(w, r)
			return
		}
		dev := "CDROM"
		if strings.Contains(body, `DEVICE="FLOPPY"`) {
			dev = "FLOPPY"
		}
		switch {
		case strings.Contains(body, "<INSERT_VIRTUAL_MEDIA"):
			if attach {
				i := strings.Index(body, `IMAGE_URL="`) + len(`IMAGE_URL="`)
				state[dev] = body[i : i+strings.Index(body[i:], `"`)]
			}
			io.WriteString(w, ilo3Reply("0x0000", "No error", ""))
		case strings.Contains(body, "<EJECT_VIRTUAL_MEDIA"):
			if state[dev] == "" {
				io.WriteString(w, ilo3Reply("0x0001", "No image present in the Virtual Media drive.", ""))
				return
			}
			delete(state, dev)
			io.WriteString(w, ilo3Reply("0x0000", "No error", ""))
		case strings.Contains(body, "<GET_VM_STATUS"):
			io.WriteString(w, ilo3Reply("0x0000", "No error", vmStatusXML(dev, state[dev] != "", state[dev])))
		default:
			io.WriteString(w, ilo3Reply("0x0000", "No error", ""))
		}
	})
	return host, rec, state
}

func TestILO3RIBCL(t *testing.T) {
	host, rec, state := fakeILO(t, true)
	p := NonRedfishBackend(host, "admin", "pa&ss<", 11)
	if err := p.Insert("http://10.0.0.5:8080/images/x.iso"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got := rec.all()
	for _, want := range []string{
		`POST /ribcl <?xml version="1.0"?>`,
		`<RIBCL VERSION="2.0"><LOGIN USER_LOGIN="admin" PASSWORD="pa&ss<"><RIB_INFO MODE="write"><INSERT_VIRTUAL_MEDIA DEVICE="CDROM" IMAGE_URL="http://10.0.0.5:8080/images/x.iso"/></RIB_INFO></LOGIN></RIBCL>`,
		`<SET_VM_STATUS DEVICE="CDROM"><VM_BOOT_OPTION value="CONNECT"/><VM_WRITE_PROTECT value="YES"/></SET_VM_STATUS>`,
		`<RIB_INFO MODE="read"><GET_VM_STATUS DEVICE="CDROM"/>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s\nin:\n%s", want, got)
		}
	}
	// Eject: only the inserted CDROM, after reading both statuses.
	if err := p.Eject(); err != nil {
		t.Fatalf("eject: %v", err)
	}
	got = rec.all()
	if !strings.Contains(got, `<EJECT_VIRTUAL_MEDIA DEVICE="CDROM"/>`) || strings.Contains(got, `<EJECT_VIRTUAL_MEDIA DEVICE="FLOPPY"/>`) {
		t.Errorf("eject should touch the CDROM only:\n%s", got)
	}
	if len(state) != 0 {
		t.Errorf("still inserted: %v", state)
	}
	// Nothing inserted: eject sends no EJECT at all and succeeds.
	before := strings.Count(rec.all(), "<EJECT_VIRTUAL_MEDIA")
	if err := p.Eject(); err != nil || strings.Count(rec.all(), "<EJECT_VIRTUAL_MEDIA") != before {
		t.Errorf("eject of nothing: %v", err)
	}
	if ribclDevice("http://x/dos.img") != "FLOPPY" {
		t.Error("floppy image device")
	}
	if err := NonRedfishBackend(host, "admin", `a"b`, 11).Insert("http://x/y.iso"); err == nil {
		t.Error("quote in password accepted")
	}
}

func TestILO3NotAttached(t *testing.T) {
	host, _, _ := fakeILO(t, false)
	if err := NonRedfishBackend(host, "admin", "pw", 11).Insert("http://x/y.iso"); err == nil || !strings.Contains(err.Error(), "not inserted") {
		t.Fatalf("an image the iLO did not attach: %v", err)
	}
}

func TestILO3Errors(t *testing.T) {
	host := tlsHost(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, ilo3Reply("0x005F", "Login failed.", ""))
	})
	err := (&ilo3Provider{host: host, user: "u", pass: "p"}).Insert("http://x/y.iso")
	if err == nil || !strings.Contains(err.Error(), "0x005f: Login failed.") {
		t.Fatalf("login failure: %v", err)
	}
	host2 := tlsHost(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if err := (&ilo3Provider{host: host2, user: "u", pass: "p"}).Insert("http://x/y.iso"); err == nil || !strings.Contains(err.Error(), "iLO 2 or older") {
		t.Fatalf("404: %v", err)
	}
}

// --- Cisco CIMC: IMC XML API ---

func TestCIMCMapping(t *testing.T) {
	for in, want := range map[string][4]string{
		"http://10.0.0.5:8080/images/x.iso": {"www", "http://10.0.0.5:8080/images/", "x.iso", "noauto"},
		"https://srv/x.iso":                 {"www", "https://srv/", "x.iso", "noauto"},
		"nfs://10.1.1.1/export/isos/y.iso":  {"nfs", "10.1.1.1:/export/isos", "y.iso", ""},
		"cifs://srv/share/z.iso":            {"cifs", "//srv/share", "z.iso", ""},
	} {
		m, s, f, o, err := cimcMapping(in)
		if err != nil || [4]string{m, s, f, o} != want {
			t.Errorf("%s: %v %v err %v", in, [4]string{m, s, f, o}, want, err)
		}
	}
	if _, _, _, _, err := cimcMapping("ftp://x/y.iso"); err == nil {
		t.Error("ftp accepted")
	}
	if v := cimcVolumeName("Ubuntu Server 24.04 (amd64).iso"); v != "Ubuntu_Server_24.04__amd64_.iso" {
		t.Errorf("volume name %q", v)
	}
	if v := cimcVolumeName(strings.Repeat("a", 60)); len(v) != 47 {
		t.Errorf("volume name length %d", len(v))
	}
}

func TestCIMCInsertEject(t *testing.T) {
	old, oldPoll := cimcMapWait, cimcMapPoll
	cimcMapWait, cimcMapPoll = 2*time.Second, 10*time.Millisecond
	t.Cleanup(func() { cimcMapWait, cimcMapPoll = old, oldPoll })
	rec := &recorder{}
	polls := 0
	host := tlsHost(t, func(w http.ResponseWriter, r *http.Request) {
		body := rec.add(r)
		switch {
		case strings.HasPrefix(body, "<aaaLogin"):
			io.WriteString(w, `<aaaLogin cookie="" response="yes" outCookie="1234/abcd" outRefreshPeriod="600"/>`)
		case strings.HasPrefix(body, "<aaaLogout"):
			io.WriteString(w, `<aaaLogout cookie="" response="yes" outStatus="success"/>`)
		case strings.HasPrefix(body, "<configConfMo"):
			io.WriteString(w, `<configConfMo dn="x" cookie="1234/abcd" response="yes"><outConfig/></configConfMo>`)
		case strings.HasPrefix(body, "<configResolveDn"):
			polls++
			st := "In Progress"
			if polls > 1 {
				st = "OK"
			}
			io.WriteString(w, `<configResolveDn cookie="1234/abcd" response="yes" dn="x"><outConfig><commVMediaMap dn="sys/svc-ext/vmedia-svc/vmmap-x.iso" volumeName="x.iso" mappingStatus="`+st+`"/></outConfig></configResolveDn>`)
		case strings.HasPrefix(body, "<configResolveChildren"):
			io.WriteString(w, `<configResolveChildren cookie="1234/abcd" response="yes"><outConfigs><commVMediaMap dn="sys/svc-ext/vmedia-svc/vmmap-x.iso" volumeName="x.iso" mappingStatus="OK"/></outConfigs></configResolveChildren>`)
		}
	})
	p := NonRedfishBackend(host, "admin", "pw", 9)
	if err := p.Insert("http://10.0.0.5:8080/images/x.iso"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got := rec.all()
	for _, want := range []string{
		`POST /nuova <aaaLogin inName="admin" inPassword="pw"/>`,
		`<configConfMo cookie="1234/abcd" dn="sys/svc-ext/vmedia-svc/vmmap-x.iso" inHierarchical="false"><inConfig><commVMediaMap dn="sys/svc-ext/vmedia-svc/vmmap-x.iso" volumeName="x.iso" map="www" remoteShare="http://10.0.0.5:8080/images/" remoteFile="x.iso" mountOptions="noauto" status="created"/></inConfig></configConfMo>`,
		`<aaaLogout cookie="1234/abcd" inCookie="1234/abcd"/>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s\nin:\n%s", want, got)
		}
	}
	if polls < 2 {
		t.Errorf("polled %d times; In Progress should be waited out", polls)
	}
	if err := p.Eject(); err != nil {
		t.Fatalf("eject: %v", err)
	}
	if !strings.Contains(rec.all(), `<commVMediaMap dn="sys/svc-ext/vmedia-svc/vmmap-x.iso" status="removed"/>`) {
		t.Errorf("no removal:\n%s", rec.all())
	}
}

func TestCIMCErrors(t *testing.T) {
	host := tlsHost(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<aaaLogin cookie="" response="yes" errorCode="551" invocationResult="unidentified-fail" errorDescr="Authorization required"/>`)
	})
	if err := NonRedfishBackend(host, "admin", "bad", 9).Insert("http://x/y.iso"); err == nil || !strings.Contains(err.Error(), "551") {
		t.Fatalf("login error: %v", err)
	}
}

// --- Lenovo XCC: PATCH on EXT slots ---

func fakeXCC(t *testing.T, xcc2 bool, ext1Inserted bool) (string, *recorder) {
	rec := &recorder{}
	col := "/redfish/v1/Managers/1/VirtualMedia"
	if xcc2 {
		col = "/redfish/v1/Systems/1/VirtualMedia"
	}
	slot := func(id string, in bool) string {
		img := `null`
		if in {
			img = `"http://srv/old.iso"`
		}
		return `{"Id":"` + id + `","Inserted":` + map[bool]string{true: "true", false: "false"}[in] + `,"Image":` + img + `}`
	}
	host := tlsHost(t, func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		switch r.URL.Path {
		case col:
			io.WriteString(w, `{"Members":[{"@odata.id":"`+col+`/RDOC1"},{"@odata.id":"`+col+`/EXT1"},{"@odata.id":"`+col+`/EXT2"},{"@odata.id":"`+col+`/Remote1"}]}`)
		case col + "/EXT1":
			if r.Method == "GET" {
				io.WriteString(w, slot("EXT1", ext1Inserted))
				return
			}
			io.WriteString(w, slot("EXT1", true))
		case col + "/EXT2":
			io.WriteString(w, slot("EXT2", false))
		default:
			http.NotFound(w, r)
		}
	})
	return host, rec
}

func TestXCC(t *testing.T) {
	for _, xcc2 := range []bool{false, true} {
		host, rec := fakeXCC(t, xcc2, false)
		p := NewProvider(host, "USERID", "pw", "Lenovo", "")
		if err := p.Insert("http://10.0.0.5:8080/images/x.iso"); err != nil {
			t.Fatalf("xcc2=%v insert: %v", xcc2, err)
		}
		got := rec.all()
		if !strings.Contains(got, `PATCH `) || !strings.Contains(got, `/EXT1 {"Image":"http://10.0.0.5:8080/images/x.iso","Inserted":true,"WriteProtected":true}`) {
			t.Errorf("xcc2=%v: no PATCH of EXT1:\n%s", xcc2, got)
		}
		if strings.Contains(got, "RDOC1 {") || strings.Contains(got, "Remote1 {") {
			t.Errorf("xcc2=%v: touched a non-EXT slot:\n%s", xcc2, got)
		}
	}
	// EXT1 in use: EXT2 is used; eject clears EXT1 only.
	host, rec := fakeXCC(t, false, true)
	p := NewProvider(host, "USERID", "pw", "Lenovo", "")
	if err := p.Insert("http://srv/new.iso"); err != nil || !strings.Contains(rec.all(), `/EXT2 {"Image":"http://srv/new.iso"`) {
		t.Fatalf("second slot: %v\n%s", err, rec.all())
	}
	if err := p.Eject(); err != nil || !strings.Contains(rec.all(), `PATCH /redfish/v1/Managers/1/VirtualMedia/EXT1 {"Image":null,"Inserted":false}`) {
		t.Fatalf("eject: %v\n%s", err, rec.all())
	}
	if strings.Contains(rec.all(), `PATCH /redfish/v1/Managers/1/VirtualMedia/EXT2 {"Image":null`) {
		t.Error("ejected an empty slot")
	}
	// Credentials in a share URL go to UserName/Password.
	b := xccInsertBody("cifs://user:secret@srv/share/x.iso")
	if b["Image"] != "cifs://srv/share/x.iso" || b["UserName"] != "user" || b["Password"] != "secret" {
		t.Errorf("credentials: %v", b)
	}
}

// --- Dell iDRAC 7/8: OEM network ISO ---

func TestDellShare(t *testing.T) {
	b, err := dellShare("nfs://10.1.1.1/export/isos/x.iso")
	want := map[string]any{"IPAddress": "10.1.1.1", "ShareType": "NFS", "ShareName": "/export/isos", "ImageName": "x.iso"}
	if err != nil || !mapsEqual(b, want) {
		t.Errorf("nfs: %v err %v", b, err)
	}
	b, err = dellShare("cifs://dom%5Cuser:pw@srv/share/x.iso")
	want = map[string]any{"IPAddress": "srv", "ShareType": "CIFS", "ShareName": "share", "ImageName": "x.iso", "UserName": `dom\user`, "Password": "pw"}
	if err != nil || !mapsEqual(b, want) {
		t.Errorf("cifs: %v err %v", b, err)
	}
	if _, err := dellShare("cifs://srv/x.iso"); err == nil {
		t.Error("CIFS without a share name accepted")
	}
	if !IsShareURL("smb://srv/s/x.iso") || IsShareURL("http://srv/x.iso") {
		t.Error("IsShareURL")
	}
}

func mapsEqual(a, b map[string]any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func TestIDRACNetworkISO(t *testing.T) {
	old := dellJobWait
	dellJobWait = 10 * time.Second
	t.Cleanup(func() { dellJobWait = old })
	rec := &recorder{}
	base := "/redfish/v1/Dell/Systems/System.Embedded.1/DellOSDeploymentService"
	attached := false
	host := tlsHost(t, func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		switch r.URL.Path {
		case base:
			// As the iDRAC9 4.20 lists it (fy-capture.sh, 2026-11-24).
			io.WriteString(w, `{"Id":"DellOSDeploymentService","Actions":{`+
				`"#DellOSDeploymentService.ConnectNetworkISOImage":{"ShareType@Redfish.AllowableValues":["CIFS","NFS"],"target":"`+base+`/Actions/DellOSDeploymentService.ConnectNetworkISOImage"},`+
				`"#DellOSDeploymentService.DisconnectNetworkISOImage":{"target":"`+base+`/Actions/DellOSDeploymentService.DisconnectNetworkISOImage"},`+
				`"#DellOSDeploymentService.GetAttachStatus":{"target":"`+base+`/Actions/DellOSDeploymentService.GetAttachStatus"}}}`)
		case base + "/Actions/DellOSDeploymentService.ConnectNetworkISOImage":
			attached = true
			w.Header().Set("Location", "/redfish/v1/TaskService/Tasks/OSDeployment")
			w.WriteHeader(http.StatusAccepted)
		case "/redfish/v1/TaskService/Tasks/OSDeployment":
			io.WriteString(w, `{"TaskState":"Completed"}`)
		case base + "/Actions/DellOSDeploymentService.GetAttachStatus":
			st := "NotAttached"
			if attached {
				st = "Attached"
			}
			io.WriteString(w, `{"@Message.ExtendedInfo":[{"Message":"Successfully Completed Request","MessageId":"Base.1.5.Success"}],"DriversAttachStatus":"NotAttached","ISOAttachStatus":"`+st+`"}`)
		case base + "/Actions/DellOSDeploymentService.DisconnectNetworkISOImage":
			attached = false
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r) // the other base and standard VirtualMedia: absent, as on iDRAC 7/8
		}
	})
	p := NewProvider(host, "root", "pw", "Dell Inc.", "")
	if err := p.Insert("nfs://10.1.1.1/export/isos/x.iso"); err != nil {
		t.Fatalf("insert: %v\n%s", err, rec.all())
	}
	if !strings.Contains(rec.all(), `POST `+base+`/Actions/DellOSDeploymentService.ConnectNetworkISOImage {"IPAddress":"10.1.1.1","ImageName":"x.iso","ShareName":"/export/isos","ShareType":"NFS"}`) {
		t.Errorf("connect body:\n%s", rec.all())
	}
	if err := p.Eject(); err != nil {
		t.Fatalf("eject: %v", err)
	}
	if attached {
		t.Error("not disconnected")
	}
	// An HTTP image on this iDRAC 7/8 (no standard VirtualMedia): told to use a share.
	if err := p.Insert("http://10.0.0.5:8080/x.iso"); err == nil || !strings.Contains(err.Error(), "NFS or CIFS shares only") {
		t.Errorf("http on iDRAC 7/8: %v", err)
	}
}

// A share type the iDRAC does not list is refused before anything is sent.
func TestDellShareTypeChecked(t *testing.T) {
	o := dellOSD{actions: map[string]any{"#DellOSDeploymentService.ConnectNetworkISOImage": map[string]any{
		"ShareType@Redfish.AllowableValues": []any{"NFS"}, "target": "/x/Connect"}}}
	if ok, list := o.allows("ConnectNetworkISOImage", "ShareType", "CIFS"); ok || len(list) != 1 {
		t.Errorf("CIFS allowed: %v %v", ok, list)
	}
	if ok, _ := o.allows("ConnectNetworkISOImage", "ShareType", "nfs"); !ok {
		t.Error("NFS refused")
	}
	if o.target("ConnectNetworkISOImage") != "/x/Connect" || (dellOSD{base: "/b"}).target("GetAttachStatus") != "/b/Actions/DellOSDeploymentService.GetAttachStatus" {
		t.Error("targets")
	}
}
