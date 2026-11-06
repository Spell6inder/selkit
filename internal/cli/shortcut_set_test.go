package cli

import (
	"os"
	"strings"
	"testing"
)

func TestShortcutSet(t *testing.T) {
	s, k, calls, out := kdeFake(t, map[int][]string{keyF6: {"kwin", "x", "KWin", "Something"}})
	b := s.binder("kde").(kdeBinder)

	if code, err := s.run([]string{"set", "--menu=F9", "--repeat=F7", "--picker=wofi"}); code != 0 || err != nil {
		t.Fatalf("set: %d, %v", code, err)
	}
	keysEqual(t, "menu", k.keys["_launch"], keyF9)
	keysEqual(t, "repeat", k.keys["repeat"], keyF7)
	if !strings.HasSuffix(k.commands["_launch"], "menu -type -picker wofi") || !strings.HasSuffix(k.commands["repeat"], "repeat -type") {
		t.Errorf("commands: %v", k.commands)
	}

	// --no-type only copies, and a key added by hand stays.
	k.keys["repeat"] = append(k.keys["repeat"], keyF8)
	if code, err := s.run([]string{"set", "--menu=F9", "--repeat=F7", "--no-type"}); code != 0 || err != nil {
		t.Fatalf("no-type: %d, %v", code, err)
	}
	if strings.Contains(k.commands["_launch"], "-type") || strings.Contains(k.commands["repeat"], "-type") {
		t.Errorf("--no-type: %v", k.commands)
	}
	keysEqual(t, "repeat after set", k.keys["repeat"], keyF7, keyF8)

	out.Reset()
	if code, err := s.run([]string{"status"}); code != 0 || err != nil {
		t.Fatalf("status: %d, %v", code, err)
	}
	for _, want := range []string{"menu", "F9", "repeat", "F7, F8", "selkit\" repeat"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}

	// One key taken: neither is touched, only checked.
	before, _ := os.ReadFile(b.file())
	*calls = nil
	if code, err := s.run([]string{"set", "--menu=F5", "--repeat=F6"}); code != 1 || err == nil || !strings.Contains(err.Error(), "KWin") {
		t.Errorf("taken: %d, %v", code, err)
	}
	if after, _ := os.ReadFile(b.file()); string(after) != string(before) {
		t.Error("the file changed although a key was taken")
	}
	for _, c := range *calls {
		if !strings.Contains(c, "KGlobalAccel.action int32:") {
			t.Errorf("more than checks: %s", c)
		}
	}

	// Mistakes change nothing.
	for _, args := range [][]string{
		{"set"}, {"set", "--no-type"}, {"set", "--menu=F9", "--repeat=F9"}, {"set", "--menu=F9", "F7"}, {"set", "--nosuch"},
		{"set", "--menu=Nosuch"}, {"set", "--menu=W"}, {"remove", "menu"}, {"remove", "--nosuch"}, {"status", "x"}, {"open", "x"}, {"nosuch"}, {"F9"},
	} {
		if code, err := s.run(args); code != 2 || err == nil {
			t.Errorf("%v: %d, %v", args, code, err)
		}
	}

	out.Reset()
	if code, err := s.run([]string{"remove"}); code != 0 || err != nil {
		t.Fatalf("remove: %d, %v", code, err)
	}
	if _, err := os.Stat(b.file()); err == nil {
		t.Error("file left after remove")
	}
	if len(k.keys) != 0 {
		t.Errorf("service still has %v", k.keys)
	}
	if !strings.Contains(out.String(), s.config) {
		t.Errorf("settings folder not mentioned: %s", out)
	}
	// Twice is fine: the point is that nothing is left.
	if code, err := s.run([]string{"remove"}); code != 0 || err != nil {
		t.Errorf("remove again: %d, %v", code, err)
	}

	out.Reset()
	if code, err := s.run(nil); code != 0 || err != nil || !strings.Contains(out.String(), "no key") {
		t.Errorf("status with nothing: %d, %v, %s", code, err, out)
	}
}

func TestShortcutIcon(t *testing.T) {
	s, _, _, _ := kdeFake(t, nil)
	icon := iconFile(s.dataDir)
	if _, err := os.Stat(icon); err == nil {
		t.Fatal("an icon before anything was set")
	}
	if code, err := s.run([]string{"set", "--default"}); code != 0 || err != nil {
		t.Fatalf("set: %d, %v", code, err)
	}
	if data, err := os.ReadFile(icon); err != nil || !strings.Contains(string(data), "<svg") {
		t.Errorf("icon: %v, %.20s", err, data)
	}
	if entry, _ := os.ReadFile(s.binder("kde").(kdeBinder).file()); !strings.Contains(string(entry), "Icon=selkit\n") {
		t.Errorf("entry:\n%s", entry)
	}
	// It stays while a key is left, and goes with the last.
	if code, err := s.run([]string{"remove", "--menu"}); code != 0 || err != nil {
		t.Fatalf("remove: %d, %v", code, err)
	}
	if _, err := os.Stat(icon); err != nil {
		t.Error("icon gone with a key left")
	}
	if code, err := s.run([]string{"remove"}); code != 0 || err != nil {
		t.Fatalf("remove all: %d, %v", code, err)
	}
	if _, err := os.Stat(icon); err == nil {
		t.Error("icon left after removing everything")
	}

	// A desktop where nothing is done gets no file either.
	s.current = "sway"
	s.run([]string{"set", "--default"})
	if _, err := os.Stat(icon); err == nil {
		t.Error("icon installed for instructions only")
	}
}

func TestShortcutDefault(t *testing.T) {
	s, k, _, _ := kdeFake(t, nil)
	if code, err := s.run([]string{"set", "--default"}); code != 0 || err != nil {
		t.Fatalf("%d, %v", code, err)
	}
	menu, _ := parseKey("Ctrl+Shift+Alt+X")
	repeat, _ := parseKey("Ctrl+Shift+Alt+Z")
	keysEqual(t, "menu", k.keys["_launch"], menu.qt())
	keysEqual(t, "repeat", k.keys["repeat"], repeat.qt())

	// One of the two can differ from the default.
	if code, err := s.run([]string{"set", "--default", "--menu=F9"}); code != 0 || err != nil {
		t.Fatalf("%d, %v", code, err)
	}
	keysEqual(t, "menu", k.keys["_launch"], keyF9)
	keysEqual(t, "repeat", k.keys["repeat"], repeat.qt())

	// One alone leaves the other.
	if code, err := s.run([]string{"set", "--repeat=F7"}); code != 0 || err != nil {
		t.Fatalf("%d, %v", code, err)
	}
	keysEqual(t, "menu", k.keys["_launch"], keyF9)
	keysEqual(t, "repeat", k.keys["repeat"], keyF7)
}

func TestShortcutOtherDesktops(t *testing.T) {
	for current, want := range map[string][]string{
		"sway":     {"bindsym Ctrl+Mod1+Shift+x exec", "bindsym Ctrl+Mod1+Shift+z exec", "menu -type", "repeat -type"},
		"Hyprland": {"bind = CTRL ALT SHIFT, X, exec,", "bind = CTRL ALT SHIFT, Z, exec,"},
		"":         {"Ctrl+Alt+Shift+X", "Ctrl+Alt+Shift+Z", "menu -type", "repeat -type"},
	} {
		s, _, calls, out := kdeFake(t, nil)
		s.current = current
		if code, err := s.run([]string{"set", "--default"}); code != 1 || err != nil || len(*calls) != 0 {
			t.Errorf("%s: %d, %v, %v", current, code, err, *calls)
		}
		for _, w := range want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("%s: no %q in %s", current, w, out)
			}
		}

		// Nothing was done there, so nothing can be taken away or shown; it says so.
		out.Reset()
		if code, err := s.run([]string{"remove"}); code != 1 || err != nil || !strings.Contains(out.String(), "remove them") {
			t.Errorf("%s remove: %d, %v, %s", current, code, err, out)
		}
		out.Reset()
		if code, err := s.run(nil); code != 0 || err != nil || !strings.Contains(out.String(), "assigns no keys") {
			t.Errorf("%s status: %d, %v, %s", current, code, err, out)
		}
	}
}
