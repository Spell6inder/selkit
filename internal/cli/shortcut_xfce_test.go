package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestShortcutXfce(t *testing.T) {
	d, calls := fake(false, "xfconf-query")
	base := d.run
	listing := "/commands/custom/<Alt>F2                 xfce4-appfinder --collapsed\n" +
		"/commands/custom/override                true\n" +
		"/commands/custom/F9                      /usr/bin/selkit menu -type\n" +
		"/commands/custom/<Shift><Super>r         \"/opt/wheel kit/selkit\" repeat\n" +
		"/xfwm4/custom/<Alt>F4                    close_window_key\n"
	d.run = func(stdin, name string, args ...string) (string, error) {
		base(stdin, name, args...)
		if slices.Contains(args, "-l") {
			return listing, nil
		}
		return "", nil
	}
	var out bytes.Buffer
	s := shortcutCmd{d, "XFCE", t.TempDir(), "/usr/bin/selkit", &out, "x"}

	if code, err := s.run([]string{"set", "--menu=Ctrl+Meta+W"}); code != 0 || err != nil {
		t.Fatalf("set: %d, %v", code, err)
	}
	set := "xfconf-query -c xfce4-keyboard-shortcuts -n -t string -p /commands/custom/<Primary><Super>w -s /usr/bin/selkit menu -type <"
	if !slices.Contains(*calls, set) {
		t.Errorf("no %q in %v", set, *calls)
	}
	// The old key of the same command goes; the other Selkit command stays.
	if !slices.Contains(*calls, "xfconf-query -c xfce4-keyboard-shortcuts -p /commands/custom/F9 -r <") {
		t.Errorf("old key kept: %v", *calls)
	}
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.Contains(c, "<Shift><Super>r -r") }) {
		t.Errorf("repeat removed along with menu: %v", *calls)
	}

	for _, taken := range []string{"Alt+F4", "Alt+F2"} {
		*calls = nil
		if code, err := s.run([]string{"set", "--menu=" + taken}); code != 1 || err == nil {
			t.Errorf("%s: %d, %v", taken, code, err)
		}
		if slices.ContainsFunc(*calls, func(c string) bool { return strings.Contains(c, " -n ") || strings.Contains(c, " -r ") }) {
			t.Errorf("%s changed something: %v", taken, *calls)
		}
	}

	// The key this command already has, with the modifiers in another order, is not a conflict.
	if code, err := s.run([]string{"set", "--repeat=Meta+Shift+R"}); code != 0 || err != nil {
		t.Errorf("same key again: %d, %v", code, err)
	}

	*calls = nil
	if code, err := s.run([]string{"remove", "--repeat"}); code != 0 || err != nil ||
		!slices.Contains(*calls, "xfconf-query -c xfce4-keyboard-shortcuts -p /commands/custom/<Shift><Super>r -r <") {
		t.Errorf("remove: %d, %v, %v", code, err, *calls)
	}
}
