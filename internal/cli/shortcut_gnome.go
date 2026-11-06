package cli

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// gnomeBinder assigns keys as custom shortcuts, the ones in the keyboard settings of GNOME, through gsettings.
type gnomeBinder struct{ d *desktop }

const (
	gnomeMediaKeys = "org.gnome.settings-daemon.plugins.media-keys"
	gnomeCustom    = gnomeMediaKeys + ".custom-keybinding:"
	gnomeList      = "custom-keybindings"
	gnomeBase      = "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/"
)

// The schemas where GNOME itself keeps keys.
var gnomeKeySchemas = []string{
	"org.gnome.desktop.wm.keybindings", "org.gnome.shell.keybindings", "org.gnome.mutter.keybindings",
	"org.gnome.mutter.wayland.keybindings", gnomeMediaKeys,
}

func gnomePath(name string) string { return gnomeBase + "selkit-" + name + "/" }

func (b gnomeBinder) gsettings(args ...string) (string, error) {
	if _, err := b.d.lookPath("gsettings"); err != nil {
		return "", errors.New(i18n.T("gsettings not found"))
	}
	return b.d.run("", "gsettings", args...)
}

var gvariantString = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)

// gvariantStrings reads the strings of a value printed by gsettings: 'a' or ['a', 'b'].
func gvariantStrings(value string) []string {
	var list []string
	for _, m := range gvariantString.FindAllStringSubmatch(value, -1) {
		list = append(list, strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(m[1]))
	}
	return list
}

func gvariantQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func (b gnomeBinder) paths() ([]string, error) {
	out, err := b.gsettings("get", gnomeMediaKeys, gnomeList)
	if err != nil {
		return nil, err
	}
	return gvariantStrings(out), nil
}

func (b gnomeBinder) setPaths(paths []string) error {
	value := "@as []"
	if len(paths) > 0 {
		quoted := make([]string, len(paths))
		for i, p := range paths {
			quoted[i] = gvariantQuote(p)
		}
		value = "[" + strings.Join(quoted, ", ") + "]"
	}
	_, err := b.gsettings("set", gnomeMediaKeys, gnomeList, value)
	return err
}

func (b gnomeBinder) check(k key, name string) error {
	gtk := k.gtk()
	for _, schema := range gnomeKeySchemas {
		out, err := b.gsettings("list-recursively", schema)
		if err != nil {
			if schema == gnomeMediaKeys {
				return err
			}
			continue // not every version has every schema
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			for _, accel := range gvariantStrings(line) {
				if len(fields) >= 2 && sameGtkKey(accel, gtk) {
					return errors.New(i18n.Tf("%s is already used by %s", k.String(), fields[1]))
				}
			}
		}
	}
	paths, err := b.paths()
	if err != nil {
		return err
	}
	for _, path := range paths {
		if path == gnomePath(name) {
			continue
		}
		out, err := b.gsettings("get", gnomeCustom+path, "binding")
		if err != nil {
			return err
		}
		for _, accel := range gvariantStrings(out) {
			if sameGtkKey(accel, gtk) {
				title, _ := b.gsettings("get", gnomeCustom+path, "name")
				return errors.New(i18n.Tf("%s is already used by %s", k.String(), strings.Join(gvariantStrings(title), "")))
			}
		}
	}
	return nil
}

func (b gnomeBinder) set(k key, a shortcutAction) error {
	path := gnomePath(a.name)
	for _, x := range [][2]string{{"name", "Selkit " + a.name}, {"command", a.command}, {"binding", k.gtk()}} {
		if _, err := b.gsettings("set", gnomeCustom+path, x[0], gvariantQuote(x[1])); err != nil {
			return err
		}
	}
	paths, err := b.paths()
	if err != nil {
		return err
	}
	if slices.Contains(paths, path) {
		return nil
	}
	return b.setPaths(append(paths, path))
}

func (b gnomeBinder) remove(name string) (bool, error) {
	paths, err := b.paths()
	if err != nil {
		return false, err
	}
	path := gnomePath(name)
	if !slices.Contains(paths, path) {
		return false, nil
	}
	if err := b.setPaths(slices.DeleteFunc(paths, func(p string) bool { return p == path })); err != nil {
		return true, err
	}
	for _, field := range []string{"name", "command", "binding"} {
		if _, err := b.gsettings("reset", gnomeCustom+path, field); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (b gnomeBinder) status(name string) (shortcutState, error) {
	paths, err := b.paths()
	if err != nil {
		return shortcutState{}, err
	}
	path := gnomePath(name)
	if !slices.Contains(paths, path) {
		return shortcutState{}, nil
	}
	var st shortcutState
	out, err := b.gsettings("get", gnomeCustom+path, "command")
	if err != nil {
		return st, err
	}
	st.command = strings.Join(gvariantStrings(out), "")
	out, err = b.gsettings("get", gnomeCustom+path, "binding")
	if err != nil {
		return st, err
	}
	for _, accel := range gvariantStrings(out) {
		if accel != "" {
			st.keys = append(st.keys, gtkKeyName(accel))
		}
	}
	return st, nil
}
