package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// kdeBinder assigns keys through one .desktop file, shown by KDE as one group, and the KDE shortcut service.
// The service lives inside KWin on Wayland, so it is talked to rather than restarted.
//
// It starts a program only for a component it found through X-KDE-Shortcuts in the file: one made over D-Bus
// with doRegister accepts a key and then does nothing. It also keeps the commands and default keys of a
// component it knows, until every action of the component is gone. So each change takes the component apart
// and builds it again, and puts back the keys it had, those added by hand in the settings included.
type kdeBinder struct {
	d   *desktop
	dir string
	exe string
}

const kdeComponent = "selkit.desktop"

// The action in the group for each command: the service calls the main one of a file _launch.
var kdeActions = map[string]string{"menu": "_launch", "repeat": "repeat"}

// How long the shortcut service gets to notice a new .desktop file.
var kdeRegisterWait = 5 * time.Second

// kdeEntry is a command in the file: what it runs, and the key Selkit gave it.
type kdeEntry struct {
	command, key string
}

func (b kdeBinder) file() string { return filepath.Join(b.dir, kdeComponent) }

// read returns the commands in the file. The file is Selkit's own, so only its own form has to be understood.
func (b kdeBinder) read() map[string]kdeEntry {
	entries := map[string]kdeEntry{}
	data, err := os.ReadFile(b.file())
	if err != nil {
		return entries
	}
	name := ""
	var cur kdeEntry
	flush := func() {
		if name != "" && cur.key != "" {
			entries[name] = cur
		}
		cur = kdeEntry{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch line {
		case "[Desktop Entry]":
			flush()
			name = "menu"
		case "[Desktop Action repeat]":
			flush()
			name = "repeat"
		default:
			if v, ok := strings.CutPrefix(line, "Exec="); ok {
				cur.command = v
			} else if v, ok := strings.CutPrefix(line, "X-KDE-Shortcuts="); ok {
				cur.key = v
			}
		}
	}
	flush()
	return entries
}

// render is the file. The main entry always exists, as the group needs it, and may have no key.
func (b kdeBinder) render(entries map[string]kdeEntry) string {
	var f strings.Builder
	menu, hasMenu := entries["menu"]
	if !hasMenu {
		menu.command = quoteExec(b.exe) + " menu"
	}
	fmt.Fprintf(&f, "[Desktop Entry]\nType=Application\nName=Selkit\nIcon=selkit\nExec=%s\nNoDisplay=true\n", menu.command)
	repeat, hasRepeat := entries["repeat"]
	if hasRepeat {
		f.WriteString("Actions=repeat;\n")
	}
	if hasMenu {
		fmt.Fprintf(&f, "X-KDE-Shortcuts=%s\n", menu.key)
	}
	if hasRepeat {
		fmt.Fprintf(&f, "\n[Desktop Action repeat]\nName=%s\nExec=%s\nX-KDE-Shortcuts=%s\n", i18n.T("Repeat the last action"), repeat.command, repeat.key)
	}
	return f.String()
}

func (b kdeBinder) call(method string, args ...string) (string, error) {
	if _, err := b.d.lookPath("dbus-send"); err != nil {
		return "", errors.New(i18n.T("dbus-send is needed: install the D-Bus tools"))
	}
	return b.d.run("", "dbus-send", append([]string{"--session", "--print-reply", "--dest=org.kde.kglobalaccel",
		"/kglobalaccel", "org.kde.KGlobalAccel." + method}, args...)...)
}

func actionID(action string) string {
	return "array:string:" + kdeComponent + "," + action + ",Selkit," + action
}

var (
	dbusString = regexp.MustCompile(`string "([^"]*)"`)
	dbusInt    = regexp.MustCompile(`int32 (-?\d+)`)
)

// holder is who has the key: the component, its action and a name to show.
func (b kdeBinder) holder(k int) (component, action, title string, err error) {
	reply, err := b.call("action", "int32:"+strconv.Itoa(k))
	if err != nil {
		return "", "", "", err
	}
	m := dbusString.FindAllStringSubmatch(reply, -1)
	if len(m) < 2 {
		return "", "", "", nil
	}
	title = m[0][1]
	if len(m) >= 4 {
		title = m[2][1] + ": " + m[3][1]
	}
	return m[0][1], m[1][1], title, nil
}

// keys are the keys the action has now.
func (b kdeBinder) keys(action string) ([]int, error) {
	reply, err := b.call("shortcut", actionID(action))
	if err != nil {
		return nil, err
	}
	var keys []int
	for _, m := range dbusInt.FindAllStringSubmatch(reply, -1) {
		if n, _ := strconv.Atoi(m[1]); n != 0 { // 0 stands for no key
			keys = append(keys, n)
		}
	}
	return keys, nil
}

func (b kdeBinder) setKeys(action string, keys []int) error {
	words := make([]string, len(keys))
	for i, k := range keys {
		words[i] = strconv.Itoa(k)
	}
	_, err := b.call("setForeignShortcut", actionID(action), "array:int32:"+strings.Join(words, ","))
	return err
}

func (b kdeBinder) rebuild() error {
	tool := b.d.find([]tool{{false, []string{"kbuildsycoca6"}}, {false, []string{"kbuildsycoca5"}}})
	if tool == nil {
		return errors.New(i18n.T("kbuildsycoca6 not found"))
	}
	_, err := b.d.run("", tool[0])
	return err
}

func (b kdeBinder) check(k key, name string) error {
	component, action, title, err := b.holder(k.qt())
	if err != nil {
		return err
	}
	if component != "" && (component != kdeComponent || action != kdeActions[name]) {
		return errors.New(i18n.Tf("%s is already used by %s", k.String(), title))
	}
	return nil
}

// current is what the component has now, for each command in the file.
func (b kdeBinder) current(entries map[string]kdeEntry) (map[string][]int, error) {
	keys := map[string][]int{}
	for name := range entries {
		list, err := b.keys(kdeActions[name])
		if err != nil {
			return nil, err
		}
		keys[name] = list
	}
	return keys, nil
}

// rewrite takes the component apart and builds it from entries with the given keys.
func (b kdeBinder) rewrite(entries map[string]kdeEntry, keys map[string][]int) error {
	if _, err := os.Stat(b.file()); err == nil {
		for _, action := range kdeActions {
			if err := b.setKeys(action, nil); err != nil {
				return err
			}
			if _, err := b.call("unregister", "string:"+kdeComponent, "string:"+action); err != nil {
				return err
			}
		}
		if err := os.Remove(b.file()); err != nil {
			return err
		}
		if err := b.rebuild(); err != nil {
			return err
		}
	}
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(b.file(), []byte(b.render(entries)), 0o644); err != nil {
		return err
	}
	if err := b.rebuild(); err != nil {
		return err
	}
	for name, e := range entries {
		k, err := parseKey(e.key)
		if err != nil {
			return err
		}
		if err := b.waitFor(k, name); err != nil {
			return err
		}
	}
	for name, list := range keys {
		if err := b.setKeys(kdeActions[name], list); err != nil {
			return err
		}
	}
	return nil
}

func (b kdeBinder) waitFor(k key, name string) error {
	for start := time.Now(); ; time.Sleep(200 * time.Millisecond) {
		component, action, _, err := b.holder(k.qt())
		if err != nil {
			return err
		}
		if component == kdeComponent && action == kdeActions[name] {
			return nil
		}
		if time.Since(start) > kdeRegisterWait {
			return errors.New(i18n.Tf("KDE did not take the shortcut %s", k.String()))
		}
	}
}

func (b kdeBinder) set(k key, a shortcutAction) error {
	entries := b.read()
	keys, err := b.current(entries)
	if err != nil {
		return err
	}
	// The new key takes the place of the one Selkit gave before; those added by hand stay.
	list := []int{k.qt()}
	var own int
	if old, ok := entries[a.name]; ok {
		if ok, err := parseKey(old.key); err == nil {
			own = ok.qt()
		}
	}
	for _, have := range keys[a.name] {
		if have != own && have != k.qt() {
			list = append(list, have)
		}
	}
	keys[a.name] = list
	entries[a.name] = kdeEntry{a.command, k.String()}
	return b.rewrite(entries, keys)
}

func (b kdeBinder) remove(name string) (bool, error) {
	entries := b.read()
	if _, ok := entries[name]; !ok {
		return false, nil
	}
	keys, err := b.current(entries)
	if err != nil {
		return true, err
	}
	delete(entries, name)
	delete(keys, name)
	return true, b.rewrite(entries, keys)
}

func (b kdeBinder) status(name string) (shortcutState, error) {
	e, ok := b.read()[name]
	if !ok {
		return shortcutState{}, nil
	}
	codes, err := b.keys(kdeActions[name])
	if err != nil {
		return shortcutState{}, err
	}
	st := shortcutState{command: e.command}
	for _, code := range codes {
		st.keys = append(st.keys, keyName(code))
	}
	return st, nil
}
