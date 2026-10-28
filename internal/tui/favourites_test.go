package tui

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/config"
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
	f := NewFavouritesDialog(nil)
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
	if f.formFocus != favFocusSave {
		t.Fatalf("focus after three tabs = %d, want Save", f.formFocus)
	}
	changed, _, _ := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	want := config.Favourite{Name: "web-01", IP: "10.0.0.5", Username: "admin"}
	if !changed || len(f.items) != 1 || f.items[0] != want {
		t.Fatalf("saved %+v (changed=%v), want %+v", f.items, changed, want)
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
