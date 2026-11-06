package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestPickerOrder(t *testing.T) {
	first := func(wayland, gnome, display bool, installed ...string) string {
		d, _ := fake(wayland, installed...)
		d.gnome, d.display = gnome, display
		name := ""
		d.runMenu = func(stdin, n string, a ...string) (string, error) { name = n; return "", nil }
		d.Pick([]menuLine{{text: "a"}}, "")
		return name
	}
	all := []string{"fuzzel", "wofi", "rofi", "zenity"}
	for _, c := range []struct {
		name                 string
		wayland, gnome, disp bool
		installed            []string
		want                 string
	}{
		{"X11", false, false, true, all, "rofi"},
		{"X11 on GNOME", false, true, true, all, "rofi"},
		{"Wayland with Xwayland", true, false, true, all, "fuzzel"},
		{"Wayland without Xwayland", true, false, false, all, "fuzzel"},
		{"Wayland without fuzzel", true, false, true, []string{"wofi", "rofi"}, "wofi"},
		{"Wayland with only rofi", true, false, true, []string{"rofi"}, "rofi"},
		{"GNOME", true, true, true, all, "rofi"},
		{"GNOME without Xwayland", true, true, false, all, "zenity"},
		{"GNOME without rofi", true, true, true, []string{"fuzzel", "wofi", "zenity"}, "zenity"},
	} {
		if got := first(c.wayland, c.gnome, c.disp, c.installed...); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}

	// Asked for by name, it runs wherever.
	d, _ := fake(true, "fuzzel")
	d.gnome = true
	name := ""
	d.runMenu = func(stdin, n string, a ...string) (string, error) { name = n; return "", nil }
	d.Pick([]menuLine{{text: "a"}}, "fuzzel")
	if name != "fuzzel" {
		t.Errorf("by name: %s", name)
	}
}

func TestMenuIcon(t *testing.T) {
	lines := []menuLine{{text: "a"}, {text: "h", header: true}, {text: "b", marked: true}}
	pick := func(hasIcon, wayland bool, installed string) (string, []string) {
		d, _ := fake(wayland, installed)
		d.hasIcon = hasIcon
		var in string
		var args []string
		d.runMenu = func(stdin, name string, a ...string) (string, error) {
			in, args = stdin, a
			return "", nil
		}
		d.Pick(lines, "")
		return in, args
	}

	// rofi: the logo in the search line, none on the rows.
	in, args := pick(true, false, "rofi")
	if in != "a\nh\x00nonselectable\x1ftrue\nb\n" {
		t.Errorf("rofi input %q", in)
	}
	at := slices.Index(args, "-theme-str")
	if at < 0 || at+1 >= len(args) || !strings.Contains(args[at+1], `filename: "selkit"`) || !strings.Contains(strings.Join(args, " "), "-a 2") {
		t.Errorf("rofi args %q", args)
	}
	if strings.Contains(strings.Join(args, " "), "-show-icons") {
		t.Errorf("icons on the rows: %q", args)
	}
	// Without the icon installed, the search line stays as it is.
	if _, args := pick(false, false, "rofi"); slices.Contains(args, "-theme-str") {
		t.Errorf("no icon installed: %q", args)
	}

	for _, name := range []string{"fuzzel", "wofi", "zenity"} {
		in, args := pick(true, name != "zenity", name)
		if in != "a\nh\nb\n" || strings.Contains(strings.Join(args, " "), "selkit\"") {
			t.Errorf("%s: %q %q", name, in, args)
		}
	}
}
