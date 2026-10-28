package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/config"
	"github.com/knightmare2600/fyrtaarn/internal/discovery"
)

func TestFavouriteLabel(t *testing.T) {
	cases := []struct {
		fav  config.Favourite
		want string
	}{
		{config.Favourite{IP: "10.0.0.5"}, "10.0.0.5"},
		{config.Favourite{IP: "10.0.0.5", Username: "admin"}, "10.0.0.5 (admin)"},
		{config.Favourite{Name: "web-01", IP: "10.0.0.5"}, "web-01 — 10.0.0.5"},
		{config.Favourite{Name: "web-01", IP: "10.0.0.5", Username: "admin"}, "web-01 — 10.0.0.5 (admin)"},
	}
	for _, c := range cases {
		if got := favouriteLabel(c.fav); got != c.want {
			t.Errorf("favouriteLabel(%+v) = %q, want %q", c.fav, got, c.want)
		}
	}
}

func typeInto(f *FavouritesDialog, s string) {
	for _, r := range s {
		f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestFavouritesAddWithName(t *testing.T) {
	f := NewFavouritesDialog(nil, nil)
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !f.editing || f.formFocus != favFocusName {
		t.Fatalf("add form: editing=%v focus=%d, want name field focused", f.editing, f.formFocus)
	}
	typeInto(f, "web-01")
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeInto(f, "10.0.0.5")
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeInto(f, "admin")
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	f.Update(tea.KeyMsg{Type: tea.KeyTab}) // leave password empty
	if f.formFocus != favFocusSave {
		t.Fatalf("focus after four tabs = %d, want Save", f.formFocus)
	}
	changed, _, _ := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	want := config.Favourite{Name: "web-01", IP: "10.0.0.5", Username: "admin"}
	if !changed || len(f.items) != 1 || f.items[0] != want {
		t.Fatalf("saved %+v (changed=%v), want %+v", f.items, changed, want)
	}
	if c := f.TakeCreds(); len(c) != 0 {
		t.Errorf("empty password box on add queued %+v; it must not touch the session cache", c)
	}

	// Editing pre-fills the name.
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := f.nameInput.Value(); got != "web-01" {
		t.Errorf("edit form name = %q, want %q", got, "web-01")
	}
}

func TestFavouriteWithoutNameStillLoads(t *testing.T) {
	var fav config.Favourite
	if err := json.Unmarshal([]byte(`{"ip":"10.0.0.5","username":"admin"}`), &fav); err != nil {
		t.Fatal(err)
	}
	if fav.Name != "" || favouriteLabel(fav) != "10.0.0.5 (admin)" {
		t.Errorf("pre-name config entry: %+v label %q", fav, favouriteLabel(fav))
	}
}

// formTo fills the add form and leaves focus on Save.
func formTo(f *FavouritesDialog, ip, pass string) {
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeInto(f, ip)
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeInto(f, "root")
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeInto(f, pass)
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
}

func TestFavouritePasswordSessionOnly(t *testing.T) {
	f := NewFavouritesDialog(nil, nil)
	formTo(f, "10.0.0.7", "calvin")
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})

	got := f.TakeCreds()
	want := []favCred{{ip: "10.0.0.7", user: "root", pass: "calvin"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("creds = %+v, want %+v", got, want)
	}
	if c := f.TakeCreds(); c != nil {
		t.Errorf("TakeCreds did not clear: %+v", c)
	}
	b, _ := json.Marshal(f.items)
	if strings.Contains(string(b), "calvin") {
		t.Errorf("password reached the persisted favourite: %s", b)
	}
}

func TestFavouritePasswordPrefillAndClear(t *testing.T) {
	cache := map[string]string{"10.0.0.7": "calvin"}
	f := NewFavouritesDialog([]config.Favourite{{IP: "10.0.0.7", Username: "root"}},
		func(ip string) (string, bool) { p, ok := cache[ip]; return p, ok })

	f.Update(tea.KeyMsg{Type: tea.KeyEnter}) // edit entry 0
	if got := f.passInput.Value(); got != "calvin" {
		t.Fatalf("password not pre-filled from session cache: %q", got)
	}
	f.passInput.SetValue("") // user clears the box
	f.formFocus = favFocusSave
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})

	got := f.TakeCreds()
	if len(got) != 1 || got[0].ip != "10.0.0.7" || got[0].pass != "" {
		t.Fatalf("clearing a pre-filled password: creds = %+v, want one delete for 10.0.0.7", got)
	}
}

func TestDirectHostVendorReplaced(t *testing.T) {
	a := &App{}
	idx, enrich := a.addDirectHost("10.0.0.9")
	if enrich == nil || a.results[idx].Vendor != directVendor {
		t.Fatalf("new direct host: vendor %q, enrich cmd nil=%v", a.results[idx].Vendor, enrich == nil)
	}
	if again, cmd := a.addDirectHost("10.0.0.9"); again != idx || cmd != nil {
		t.Fatalf("re-adding existing host: idx %d cmd nil=%v", again, cmd == nil)
	}

	a.Update(directHostEnrichedMsg{host: discovery.HostResult{
		IP: "10.0.0.9", Hostname: "idrac-01", HasRedfish: true,
		RedfishVersion: "1.11.0", RedfishManufacturer: "Dell Inc.", RedfishModel: "PowerEdge R640",
	}})
	r := a.results[idx]
	if r.Vendor != "Dell Inc." || !r.HasRedfish || r.RedfishManufacturer != "Dell Inc." || r.Hostname != "idrac-01" {
		t.Fatalf("after enrichment: %+v", r)
	}

	// A real vendor is never overwritten by a later source.
	a.applyDirectVendor("10.0.0.9", "DELL Inc")
	if a.results[idx].Vendor != "Dell Inc." {
		t.Errorf("vendor overwritten: %q", a.results[idx].Vendor)
	}
}

func TestDirectHostVendorFromMCInfoWhenNoRedfish(t *testing.T) {
	a := &App{}
	idx, _ := a.addDirectHost("10.0.0.10")
	a.Update(directHostEnrichedMsg{host: discovery.HostResult{IP: "10.0.0.10"}})
	if a.results[idx].Vendor != directVendor {
		t.Fatalf("no Redfish data must leave placeholder, got %q", a.results[idx].Vendor)
	}
	a.applyDirectVendor("10.0.0.10", " DELL Inc ")
	if a.results[idx].Vendor != "DELL Inc" {
		t.Errorf("mc info vendor not applied: %q", a.results[idx].Vendor)
	}
}
