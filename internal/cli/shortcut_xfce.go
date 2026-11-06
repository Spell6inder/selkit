package cli

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// xfceBinder assigns keys through xfconf; Xfce picks up the change by itself.
type xfceBinder struct{ d *desktop }

const xfceCommands = "/commands/custom/"

// gtkNames are the keys whose GTK name differs from the Qt one.
var gtkNames = map[string]string{"Space": "space", "PageUp": "Page_Up", "PageDown": "Page_Down", "Backspace": "BackSpace"}

// gtk is the form Xfce keeps in its property names, such as <Primary><Super>w.
func (k key) gtk() string {
	var b strings.Builder
	for _, m := range []struct {
		bit  int
		name string
	}{{modCtrl, "<Primary>"}, {modAlt, "<Alt>"}, {modShift, "<Shift>"}, {modMeta, "<Super>"}} {
		if k.mods&m.bit != 0 {
			b.WriteString(m.name)
		}
	}
	name := k.name
	if len(name) == 1 {
		name = strings.ToLower(name)
	} else if g, ok := gtkNames[name]; ok {
		name = g
	}
	return b.String() + name
}

var gtkModifier = regexp.MustCompile(`<[^>]+>`)

// sameGtkKey compares two accelerators, whose modifiers can come in any order and whose letters in either case.
func sameGtkKey(a, b string) bool {
	norm := func(s string) string {
		mods := gtkModifier.FindAllString(strings.NewReplacer("<primary>", "<control>", "<ctrl>", "<control>").Replace(strings.ToLower(s)), -1)
		slices.Sort(mods)
		return strings.Join(mods, "") + strings.ToLower(gtkModifier.ReplaceAllString(s, ""))
	}
	return norm(a) == norm(b)
}

func (b xfceBinder) query(args ...string) (string, error) {
	return b.d.run("", "xfconf-query", append([]string{"-c", "xfce4-keyboard-shortcuts"}, args...)...)
}

// entries lists the properties of the channel with their values. A key name can contain no spaces.
func (b xfceBinder) entries() (map[string]string, error) {
	if _, err := b.d.lookPath("xfconf-query"); err != nil {
		return nil, errors.New(i18n.T("xfconf-query not found"))
	}
	out, err := b.query("-l", "-v")
	if err != nil {
		return nil, err
	}
	list := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if path, value, ok := strings.Cut(line, " "); ok {
			list[path] = strings.TrimSpace(value)
		}
	}
	return list, nil
}

// ours tells whether a property value is the Selkit command with this name.
func ours(value, name string) bool {
	return regexp.MustCompile(`selkit"? ` + regexp.QuoteMeta(name) + `( |$)`).MatchString(value)
}

// removeOurs takes away the keys of this command.
func (b xfceBinder) removeOurs(list map[string]string, name string) (bool, error) {
	found := false
	for path, value := range list {
		if strings.HasPrefix(path, xfceCommands) && ours(value, name) {
			found = true
			if _, err := b.query("-p", path, "-r"); err != nil {
				return found, err
			}
		}
	}
	return found, nil
}

func (b xfceBinder) check(k key, name string) error {
	list, err := b.entries()
	if err != nil {
		return err
	}
	gtk := k.gtk()
	for path, value := range list {
		for _, prefix := range []string{xfceCommands, "/xfwm4/custom/"} {
			if accel, ok := strings.CutPrefix(path, prefix); ok && sameGtkKey(accel, gtk) && !ours(value, name) {
				return errors.New(i18n.Tf("%s is already used by %s", k.String(), value))
			}
		}
	}
	return nil
}

func (b xfceBinder) set(k key, a shortcutAction) error {
	list, err := b.entries()
	if err != nil {
		return err
	}
	if _, err := b.removeOurs(list, a.name); err != nil {
		return err
	}
	_, err = b.query("-n", "-t", "string", "-p", xfceCommands+k.gtk(), "-s", a.command)
	return err
}

func (b xfceBinder) remove(name string) (bool, error) {
	list, err := b.entries()
	if err != nil {
		return false, err
	}
	return b.removeOurs(list, name)
}

func (b xfceBinder) status(name string) (shortcutState, error) {
	list, err := b.entries()
	if err != nil {
		return shortcutState{}, err
	}
	var st shortcutState
	for path, value := range list {
		if accel, ok := strings.CutPrefix(path, xfceCommands); ok && ours(value, name) {
			st.command = value
			st.keys = append(st.keys, gtkKeyName(accel))
		}
	}
	slices.Sort(st.keys)
	return st, nil
}

// gtkKeyName is the form the commands take, for an accelerator as Xfce keeps it.
func gtkKeyName(accel string) string {
	var k key
	for _, m := range gtkModifier.FindAllString(strings.ToLower(accel), -1) {
		switch m {
		case "<primary>", "<control>", "<ctrl>":
			k.mods |= modCtrl
		case "<alt>":
			k.mods |= modAlt
		case "<shift>":
			k.mods |= modShift
		case "<super>", "<meta>":
			k.mods |= modMeta
		}
	}
	k.name = gtkModifier.ReplaceAllString(accel, "")
	if len(k.name) == 1 {
		k.name = strings.ToUpper(k.name)
	}
	for qt, gtk := range gtkNames {
		if gtk == k.name {
			k.name = qt
		}
	}
	return k.String()
}
