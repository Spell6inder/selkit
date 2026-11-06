package cli

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

// gnomeFake keeps gsettings values as gsettings prints them.
func gnomeFake(t *testing.T) (shortcutCmd, map[string]string, *bytes.Buffer) {
	t.Helper()
	d, _ := fake(false, "gsettings")
	values := map[string]string{gnomeMediaKeys + " " + gnomeList: "@as []"}
	schemas := map[string]string{
		"org.gnome.desktop.wm.keybindings": "org.gnome.desktop.wm.keybindings close ['<Alt>F4']\norg.gnome.desktop.wm.keybindings minimize @as []\n",
		gnomeMediaKeys:                     gnomeMediaKeys + " screensaver ['<Super>l']\n" + gnomeMediaKeys + " home ''\n",
	}
	d.run = func(stdin, name string, args ...string) (string, error) {
		switch args[0] {
		case "list-recursively":
			if s, ok := schemas[args[1]]; ok {
				return s, nil
			}
			return "", errors.New("No such schema")
		case "get":
			if v, ok := values[args[1]+" "+args[2]]; ok {
				return v + "\n", nil
			}
			return "''\n", nil
		case "set":
			values[args[1]+" "+args[2]] = args[3]
		case "reset":
			delete(values, args[1]+" "+args[2])
		}
		return "", nil
	}
	var out bytes.Buffer
	return shortcutCmd{d, "ubuntu:GNOME", t.TempDir(), "/opt/wheel kit/selkit", &out, "x"}, values, &out
}

func TestShortcutGnome(t *testing.T) {
	s, values, out := gnomeFake(t)
	menu := gnomeCustom + gnomePath("menu")
	repeat := gnomeCustom + gnomePath("repeat")
	list := gnomeMediaKeys + " " + gnomeList
	values[list] = "['" + gnomeBase + "custom0/']"
	values[gnomeCustom+gnomeBase+"custom0/ name"] = "'Terminal'"
	values[gnomeCustom+gnomeBase+"custom0/ binding"] = "'<Control><Alt>t'"

	if code, err := s.run([]string{"set", "--default"}); code != 0 || err != nil {
		t.Fatalf("set: %d, %v", code, err)
	}
	if got := values[menu+" binding"]; got != "'<Primary><Alt><Shift>x'" {
		t.Errorf("menu binding %s", got)
	}
	if got := values[repeat+" binding"]; got != "'<Primary><Alt><Shift>z'" {
		t.Errorf("repeat binding %s", got)
	}
	if got := values[menu+" command"]; got != `'"/opt/wheel kit/selkit" menu -type'` {
		t.Errorf("menu command %s", got)
	}
	want := "['" + gnomeBase + "custom0/', '" + gnomePath("menu") + "', '" + gnomePath("repeat") + "']"
	if values[list] != want {
		t.Errorf("list %s, want %s", values[list], want)
	}

	out.Reset()
	if code, err := s.run([]string{"status"}); code != 0 || err != nil {
		t.Fatalf("status: %d, %v", code, err)
	}
	for _, w := range []string{"Ctrl+Alt+Shift+X", "Ctrl+Alt+Shift+Z", "selkit\" repeat -type"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("no %q in\n%s", w, out)
		}
	}

	// Moving a key neither adds to the list nor changes anything else.
	if code, err := s.run([]string{"set", "--menu=F9"}); code != 0 || err != nil {
		t.Fatalf("move: %d, %v", code, err)
	}
	if values[list] != want || values[menu+" binding"] != "'F9'" {
		t.Errorf("after the move: %s, %s", values[list], values[menu+" binding"])
	}

	// Taken: by GNOME, by another custom shortcut in another notation, by the other command. Nothing changes.
	for taken, holder := range map[string]string{"Alt+F4": "close", "Ctrl+Alt+T": "Terminal", "F9": "Selkit menu"} {
		code, err := s.run([]string{"set", "--repeat=" + taken})
		if code != 1 || err == nil || !strings.Contains(err.Error(), holder) {
			t.Errorf("%s: %d, %v", taken, code, err)
		}
	}
	if values[repeat+" binding"] != "'<Primary><Alt><Shift>z'" {
		t.Errorf("repeat changed: %s", values[repeat+" binding"])
	}

	// Removing leaves the shortcuts of others.
	if code, err := s.run([]string{"remove"}); code != 0 || err != nil {
		t.Fatalf("remove: %d, %v", code, err)
	}
	if values[list] != "['"+gnomeBase+"custom0/']" {
		t.Errorf("list after remove: %s", values[list])
	}
	for _, field := range []string{"name", "command", "binding"} {
		if _, left := values[menu+" "+field]; left {
			t.Errorf("menu %s left", field)
		}
	}
	if code, err := s.run([]string{"remove"}); code != 0 || err != nil {
		t.Errorf("remove again: %d, %v", code, err)
	}

	// The last one out leaves an empty list that gsettings can parse.
	if err := (gnomeBinder{s.d}).setPaths(slices.Delete([]string{"a"}, 0, 1)); err != nil || values[list] != "@as []" {
		t.Errorf("empty list: %v, %s", err, values[list])
	}
}

func TestGVariant(t *testing.T) {
	if got := gvariantStrings(`['a b', 'it\'s', 'c\\d']`); !slices.Equal(got, []string{"a b", "it's", `c\d`}) {
		t.Errorf("%q", got)
	}
	if got := gvariantStrings("@as []"); len(got) != 0 {
		t.Errorf("%q", got)
	}
	if got := gvariantQuote(`it's \ x`); got != `'it\'s \\ x'` {
		t.Error(got)
	}
}
