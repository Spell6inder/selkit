package cli

import (
	"bytes"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// kdeService behaves like the KDE shortcut service in what matters here: it reads a .desktop file only when it
// does not know the component yet, and from then on keeps its commands, whatever the file says.
type kdeService struct {
	keys     map[string][]int  // action → keys
	commands map[string]string // action → command
	foreign  map[int][]string  // key → the holder, for keys of other programs
	deaf     bool              // does not notice the file
}

func (k *kdeService) rebuild(b kdeBinder) {
	if k.deaf || len(k.keys) > 0 {
		return
	}
	entries := b.read()
	if _, err := os.Stat(b.file()); err != nil {
		return
	}
	k.keys["_launch"] = nil // always there, like in the real service
	for name, e := range entries {
		parsed, _ := parseKey(e.key)
		k.keys[kdeActions[name]] = []int{parsed.qt()}
		k.commands[kdeActions[name]] = e.command
	}
}

func (k *kdeService) dbus(args []string) string {
	last := args[len(args)-1]
	action := func(arg string) string { return strings.Split(arg, ",")[1] }
	switch {
	case slices.Contains(args, "org.kde.KGlobalAccel.action"):
		code, _ := strconv.Atoi(strings.TrimPrefix(last, "int32:"))
		reply := "array [\n"
		list := k.foreign[code]
		for a, keys := range k.keys {
			if slices.Contains(keys, code) {
				list = []string{kdeComponent, a, "Selkit", a}
			}
		}
		for _, s := range list {
			reply += "   string \"" + s + "\"\n"
		}
		return reply + "]\n"
	case slices.Contains(args, "org.kde.KGlobalAccel.shortcut"):
		reply := "array [\n"
		for _, code := range k.keys[action(last)] {
			reply += "   int32 " + strconv.Itoa(code) + "\n"
		}
		return reply + "]\n"
	case slices.Contains(args, "org.kde.KGlobalAccel.setForeignShortcut"):
		var keys []int
		if list := strings.TrimPrefix(last, "array:int32:"); list != "" {
			for _, word := range strings.Split(list, ",") {
				n, _ := strconv.Atoi(word)
				keys = append(keys, n)
			}
		}
		if _, known := k.keys[action(args[len(args)-2])]; known {
			k.keys[action(args[len(args)-2])] = keys
		}
	case slices.Contains(args, "org.kde.KGlobalAccel.unregister"):
		delete(k.keys, strings.TrimPrefix(last, "string:"))
	}
	return ""
}

func kdeFake(t *testing.T, foreign map[int][]string) (shortcutCmd, *kdeService, *[]string, *bytes.Buffer) {
	t.Helper()
	old := kdeRegisterWait
	kdeRegisterWait = 0
	t.Cleanup(func() { kdeRegisterWait = old })

	d, calls := fake(false, "dbus-send", "kbuildsycoca6", "systemsettings")
	k := &kdeService{map[string][]int{}, map[string]string{}, foreign, false}
	var out bytes.Buffer
	s := shortcutCmd{d, "KDE", t.TempDir(), "/opt/wheel kit/selkit", &out, "/home/u/.config/selkit"}
	base := d.run
	d.run = func(stdin, name string, args ...string) (string, error) {
		base(stdin, name, args...)
		switch name {
		case "kbuildsycoca6":
			k.rebuild(s.binder("kde").(kdeBinder))
		case "dbus-send":
			return k.dbus(args), nil
		}
		return "", nil
	}
	return s, k, calls, &out
}

const (
	keyF9 = 0x01000038
	keyF8 = 0x01000037
	keyF7 = 0x01000036
	keyF6 = 0x01000035
	keyF5 = 0x01000034
)

func keysEqual(t *testing.T, what string, got []int, want ...int) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: keys %#x, want %#x", what, got, want)
	}
}

func TestShortcutKDE(t *testing.T) {
	s, k, _, _ := kdeFake(t, map[int][]string{keyF5: {"kwin", "Close", "KWin", "Close"}})
	b := s.binder("kde").(kdeBinder)
	run := func(args ...string) {
		t.Helper()
		if code, err := s.run(args); code != 0 || err != nil {
			t.Fatalf("%v: %d, %v", args, code, err)
		}
	}

	run("set", "--menu=F9")
	keysEqual(t, "menu", k.keys["_launch"], keyF9)
	data, _ := os.ReadFile(b.file())
	for _, want := range []string{"Name=Selkit\n", "Exec=\"/opt/wheel kit/selkit\" menu -type\n", "X-KDE-Shortcuts=F9\n", "NoDisplay=true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no %q in\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "Actions=") {
		t.Errorf("no repeat yet:\n%s", data)
	}

	// Into the same group, with the menu left as it was.
	run("set", "--repeat=F7")
	keysEqual(t, "repeat", k.keys["repeat"], keyF7)
	keysEqual(t, "menu after repeat", k.keys["_launch"], keyF9)
	data, _ = os.ReadFile(b.file())
	if !strings.Contains(string(data), "Actions=repeat;\n") || !strings.Contains(string(data), "[Desktop Action repeat]\n") {
		t.Errorf("no group:\n%s", data)
	}
	if !strings.HasSuffix(k.commands["repeat"], "repeat -type") {
		t.Errorf("repeat runs %q", k.commands["repeat"])
	}

	// A key added by hand in the settings survives a new key, for this action and the other one.
	k.keys["_launch"] = append(k.keys["_launch"], keyF6)
	run("set", "--menu=F8")
	keysEqual(t, "moved menu", k.keys["_launch"], keyF8, keyF6)
	keysEqual(t, "repeat after the move", k.keys["repeat"], keyF7)

	// A new command reaches the service, which only reads the file for a new component, and keeps the keys too.
	run("set", "--menu=F8", "--clipboard")
	if !strings.HasSuffix(k.commands["_launch"], "menu -type -clipboard") {
		t.Errorf("menu runs %q", k.commands["_launch"])
	}
	keysEqual(t, "menu, new command", k.keys["_launch"], keyF8, keyF6)

	// A key taken elsewhere, also by the other command, is refused, and nothing changes.
	before, _ := os.ReadFile(b.file())
	for _, taken := range []string{"F5", "F7"} {
		code, err := s.run([]string{"set", "--menu=" + taken})
		if code != 1 || err == nil {
			t.Errorf("%s: %d, %v", taken, code, err)
		}
	}
	if after, _ := os.ReadFile(b.file()); string(after) != string(before) {
		t.Error("the file changed for a key that is taken")
	}
	// A key added by hand to this command is not a conflict: it just becomes the Selkit one.
	run("set", "--menu=F6", "--clipboard")
	keysEqual(t, "hand key taken over", k.keys["_launch"], keyF6)

	// Removing one leaves the other, with its keys; a missing one is not an error.
	k.keys["repeat"] = append(k.keys["repeat"], keyF9)
	run("remove", "--menu")
	keysEqual(t, "repeat after removing the menu", k.keys["repeat"], keyF7, keyF9)
	if len(k.keys["_launch"]) != 0 {
		t.Errorf("menu keys left: %#x", k.keys["_launch"])
	}
	if entries := b.read(); len(entries) != 1 || entries["repeat"].key != "F7" {
		t.Errorf("file: %v", entries)
	}
	run("remove", "--menu")

	// Removing the last one leaves nothing.
	run("remove", "--repeat")
	if _, err := os.Stat(b.file()); err == nil {
		t.Error("file left")
	}
	if len(k.keys) != 0 {
		t.Errorf("service still has %v", k.keys)
	}
}

func TestShortcutKDENotTaken(t *testing.T) {
	s, k, _, _ := kdeFake(t, nil)
	k.deaf = true
	if code, err := s.run([]string{"set", "--menu=F9"}); code != 1 || err == nil {
		t.Errorf("an error, not a false success: %d, %v", code, err)
	}
}
