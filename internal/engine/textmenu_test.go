package engine

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/layout"
	"github.com/Spell6inder/selkit/internal/menu"
	"github.com/Spell6inder/selkit/internal/win"
)

// Shows the real menu for a few seconds and closes it with Esc, to see how it looks
// and to check that the menu gets focus. Runs only explicitly: SELKIT_MENU_TEST=1 go test ./internal/engine
// (SELKIT_MENU_TEXT replaces the sample selection)
func TestMenuVisual(t *testing.T) {
	if os.Getenv("SELKIT_MENU_TEST") == "" {
		t.Skip("SELKIT_MENU_TEST not set")
	}
	m, err := menu.Start()
	if err != nil {
		t.Fatal(err)
	}
	fg := win.ForegroundWindow()
	i18n.Init("uk", "")
	e := &Engine{last: &action{kind: actID, id: "snake"}}
	s := selection{text: "ghbdsn, cdsn & parseHttpResponse", fg: fg}
	if text := os.Getenv("SELKIT_MENU_TEXT"); text != "" {
		s.text = text
	}
	ls := layout.Installed()
	set := loadActions(config.Default())
	list, _, items := e.menuItems(set, s, ls)
	for i, a := range list {
		o := evaluate(set, a, s, ls)
		t.Logf("%2d %-28s %q", i+1, o.name, o.text)
	}
	go func() {
		time.Sleep(3 * time.Second)
		win.SendKeys(0x1B) // Esc
	}()
	id, active := m.Show(items, win.Point{X: 300, Y: 200})
	win.ForceForeground(fg)
	if !active {
		t.Error("menu did not get focus")
	}
	if id != 0 {
		t.Errorf("Esc should close the menu without a selection, got %d", id)
	}
}

// The action repeat would run is checked where it stands, once; the "Repeat" item itself is not.
func TestRepeatChecked(t *testing.T) {
	i18n.Init("en", "")
	e := &Engine{last: &action{kind: actID, id: "snake"}}
	s := selection{text: "parseHttpResponse fooBar"}
	_, _, items := e.menuItems(loadActions(config.Default()), s, layout.Installed())

	var checked []string
	var walk func([]menu.Item)
	walk = func(its []menu.Item) {
		for _, it := range its {
			if it.Checked {
				checked = append(checked, it.Label)
			}
			walk(it.Sub)
		}
	}
	walk(items)
	if len(checked) != 1 || checked[0] != "snake_case" {
		t.Errorf("checked: %q", checked)
	}
	if !strings.HasPrefix(items[2].Label, "Repeat") || items[2].Checked {
		t.Errorf("repeat item: %+v", items[2])
	}

	e.last = nil
	checked = nil
	_, _, items = e.menuItems(loadActions(config.Default()), s, layout.Installed())
	walk(items)
	if len(checked) != 0 {
		t.Errorf("checked without a last action: %q", checked)
	}
}

// Repeating "next layout" should cycle, while "to a specific one" should refuse if the text is already in it.
func TestLayoutRepeat(t *testing.T) {
	ls := layout.Installed()
	if len(ls) != 2 {
		t.Skip("exactly two layouts required")
	}
	next := action{kind: actLayout}
	s := selection{text: "ghbdsn"}
	o1 := evaluate(actions.Set{}, next, s, ls)
	o2 := evaluate(actions.Set{}, next, selection{text: o1.text}, ls)
	if o1.why != "" || o2.why != "" || o2.text != s.text {
		t.Errorf("cycle: %q → %+v → %+v", s.text, o1, o2)
	}
	to := action{kind: actLayout, to: o1.layout}
	if o := evaluate(actions.Set{}, to, selection{text: o1.text}, ls); o.why == "" {
		t.Errorf("text already in target but the action did not refuse: %+v", o)
	}
	tr := action{kind: actID, id: "translit"}
	lat := evaluate(actions.Set{Lang: "en"}, tr, selection{text: "Щука"}, ls)
	back := evaluate(actions.Set{Lang: "en"}, tr, selection{text: lat.text}, ls)
	if lat.text != "Shchuka" || back.text != "Щука" {
		t.Errorf("Latin round trip: %q, %q", lat.text, back.text)
	}
}
