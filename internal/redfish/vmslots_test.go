package redfish

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestKindForImage(t *testing.T) {
	cases := map[string]MediaKind{
		"http://srv/ubuntu.iso":     MediaCD,
		"http://srv/fw.IMG":         MediaRemovable,
		"http://srv/dos.ima?x=1":    MediaRemovable,
		"http://srv/boot.vfd":       MediaRemovable,
		"http://srv/no-extension":   MediaCD,
		"http://srv/x.img.iso#frag": MediaCD,
	}
	for in, want := range cases {
		if got := KindForImage(in); got != want {
			t.Errorf("%s: got %v want %v", in, got, want)
		}
	}
}

// fakeVM is a BMC with a USB/floppy slot (numeric path, MediaTypes only, as
// on iLO) and a CD slot, recording which action URLs are posted to.
func fakeVM(t *testing.T, usbInserted, cdInserted bool) (host string, posts *[]string) {
	var mu sync.Mutex
	var got []string
	slot := func(types string, inserted bool, n string) string {
		return `{"MediaTypes":` + types + `,"Inserted":` + map[bool]string{true: "true", false: "false"}[inserted] +
			`,"Actions":{"#VirtualMedia.InsertMedia":{"target":"/vm/` + n + `/insert"},"#VirtualMedia.EjectMedia":{"target":"/vm/` + n + `/eject"}}}`
	}
	docs := map[string]string{
		"/redfish/v1/Managers/":                  `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/"}]}`,
		"/redfish/v1/Managers/1/":                `{"VirtualMedia":{"@odata.id":"/redfish/v1/Managers/1/VirtualMedia/"}}`,
		"/redfish/v1/Managers/1/VirtualMedia/":   `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/VirtualMedia/1/"},{"@odata.id":"/redfish/v1/Managers/1/VirtualMedia/2/"}]}`,
		"/redfish/v1/Managers/1/VirtualMedia/1/": slot(`["Floppy","USBStick"]`, usbInserted, "1"),
		"/redfish/v1/Managers/1/VirtualMedia/2/": slot(`["CD","DVD"]`, cdInserted, "2"),
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			got = append(got, r.URL.Path)
			mu.Unlock()
			return
		}
		d, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(d))
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://"), &got
}

func TestInsertBySlotKind(t *testing.T) {
	host, posts := fakeVM(t, false, false)
	p := &genericProvider{host: host, user: "u", pass: "p"}
	if err := p.Insert("http://srv/fw.img"); err != nil {
		t.Fatal(err)
	}
	if err := p.Insert("http://srv/os.iso"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*posts, ","); got != "/vm/1/insert,/vm/2/insert" {
		t.Fatalf("posts %s", got)
	}
}

func TestEjectWhatIsInserted(t *testing.T) {
	host, posts := fakeVM(t, true, false)
	if err := (&genericProvider{host: host, user: "u", pass: "p"}).Eject(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*posts, ","); got != "/vm/1/eject" {
		t.Fatalf("posts %s (want only the inserted USB slot)", got)
	}

	host, posts = fakeVM(t, false, false)
	if err := (&genericProvider{host: host, user: "u", pass: "p"}).Eject(); err != nil || len(*posts) != 0 {
		t.Fatalf("nothing inserted: err %v posts %v", err, *posts)
	}
}
