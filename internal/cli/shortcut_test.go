package cli

import (
	"strings"
	"testing"
)

func TestParseKey(t *testing.T) {
	for _, c := range []struct {
		in, out string
		qt      int
	}{
		{"F9", "F9", 0x01000038},
		{"f10", "F10", 0x01000039},
		{"Meta+Shift+W", "Meta+Shift+W", modMeta | modShift | 'W'},
		{"super+alt+1", "Meta+Alt+1", modMeta | modAlt | '1'},
		{"ctrl+pagedown", "Ctrl+PageDown", modCtrl | 0x01000017},
		{"Shift+F2", "Shift+F2", modShift | 0x01000031},
		{"Ctrl+Shift+Alt+Z", "Ctrl+Alt+Shift+Z", modCtrl | modShift | modAlt | 'Z'},
	} {
		k, err := parseKey(c.in)
		if err != nil || k.String() != c.out || k.qt() != c.qt {
			t.Errorf("%s: %v %s %#x, want %s %#x", c.in, err, k, k.qt(), c.out, c.qt)
		}
		if name := keyName(k.qt()); name != c.out {
			t.Errorf("keyName(%#x) = %s, want %s", k.qt(), name, c.out)
		}
	}
	for _, bad := range []string{"", "W", "Shift+W", "Hyper+W", "Ctrl+", "Ctrl+Nosuch"} {
		if k, err := parseKey(bad); err == nil {
			t.Errorf("%q: accepted as %s", bad, k)
		}
	}
	k, _ := parseKey("Meta+Shift+W")
	if k.bindsym() != "Mod4+Shift+w" || k.hyprMods() != "SUPER SHIFT" {
		t.Errorf("config forms: %s, %s", k.bindsym(), k.hyprMods())
	}
	for accel, want := range map[string]string{"<Primary><Alt><Shift>x": "Ctrl+Alt+Shift+X", "F9": "F9", "<Super>Page_Up": "Meta+PageUp", "<Control>space": "Ctrl+Space"} {
		if got := gtkKeyName(accel); got != want {
			t.Errorf("gtkKeyName(%s) = %s, want %s", accel, got, want)
		}
	}
}

func TestShortcutOpen(t *testing.T) {
	s, _, _, _ := kdeFake(t, nil)
	var opened string
	s.d.start = func(name string, args ...string) error {
		opened = strings.Join(append([]string{name}, args...), " ")
		return nil
	}
	if code, err := s.run([]string{"open"}); code != 0 || err != nil || opened != "systemsettings kcm_keys" {
		t.Errorf("%d, %v, opened %q", code, err, opened)
	}
	if desktopKind("ubuntu:GNOME") != "gnome" || desktopKind("KDE") != "kde" || desktopKind("") != "" {
		t.Error("desktopKind")
	}
	s.current = "sway"
	if code, err := s.run([]string{"open"}); code != 1 || err == nil {
		t.Errorf("no settings to open: %d, %v", code, err)
	}
}
