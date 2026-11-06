package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/resources"
)

const shortcutUsage = `usage: selkit shortcut [status]
       selkit shortcut set (--default | --menu=KEY | --repeat=KEY)... [--no-type] [--clipboard] [--picker=NAME]
       selkit shortcut remove [--menu] [--repeat]
       selkit shortcut open`

// The keys of "selkit shortcut set --default".
const (
	defaultMenuKey   = "Ctrl+Shift+Alt+X"
	defaultRepeatKey = "Ctrl+Shift+Alt+Z"
)

// shortcutCmd is "selkit shortcut": the keys of the menu and of repeat in the settings of the desktop.
type shortcutCmd struct {
	d       *desktop
	current string // XDG_CURRENT_DESKTOP
	dataDir string // ~/.local/share
	exe     string
	out     io.Writer
	config  string // the settings folder
}

func shortcut(args []string, configDir string, stdout, stderr io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		exe = "selkit"
	}
	s := shortcutCmd{newDesktop(), os.Getenv("XDG_CURRENT_DESKTOP"), dataHome(), exe, stdout, configDir}
	code, err := s.run(args)
	if err != nil {
		fmt.Fprintln(stderr, "selkit:", err)
	}
	return code
}

func dataHome() string {
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return data
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// iconFile is where the icon goes for the settings, notifications and menus to find it by the name selkit.
func iconFile(dataDir string) string {
	return filepath.Join(dataDir, "icons", "hicolor", "scalable", "apps", "selkit.svg")
}

func (s shortcutCmd) installIcon() error {
	path := iconFile(s.dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, resources.IconSVG, 0o644)
}

// binder assigns keys in one desktop. A desktop without one gets instructions instead.
type binder interface {
	check(k key, name string) error // fails when something other than this command has the key
	set(k key, a shortcutAction) error
	remove(name string) (bool, error) // reports whether there was a shortcut
	status(name string) (shortcutState, error)
}

// shortcutState is what a command has now; a zero one means it has no shortcut.
type shortcutState struct {
	command string
	keys    []string
}

// binding is a key with the command it should run.
type binding struct {
	k       key
	command string
}

// shortcutAction is what a key runs. A desktop keeps it under its name, "menu" or "repeat".
type shortcutAction struct {
	name    string
	command string // for the desktop to run
}

func (s shortcutCmd) run(args []string) (int, error) {
	if len(args) == 0 {
		return s.showStatus()
	}
	cmd, args := args[0], args[1:]
	switch {
	case cmd == "status" && len(args) == 0:
		return s.showStatus()
	case cmd == "open" && len(args) == 0:
		return s.open()
	case cmd == "set":
		return s.set(args)
	case cmd == "remove":
		return s.remove(args)
	}
	return 2, errors.New(shortcutUsage)
}

func newFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("shortcut", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func (s shortcutCmd) set(args []string) (int, error) {
	fs := newFlags()
	def := fs.Bool("default", false, "")
	menuKey := fs.String("menu", "", "")
	repeatKey := fs.String("repeat", "", "")
	noType := fs.Bool("no-type", false, "")
	clipboard := fs.Bool("clipboard", false, "")
	picker := fs.String("picker", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2, errors.New(shortcutUsage)
	}
	if *def {
		if *menuKey == "" {
			*menuKey = defaultMenuKey
		}
		if *repeatKey == "" {
			*repeatKey = defaultRepeatKey
		}
	}
	if *menuKey == "" && *repeatKey == "" {
		return 2, errors.New(shortcutUsage)
	}

	options := ""
	if !*noType {
		options += " -type"
	}
	if *clipboard {
		options += " -clipboard"
	}
	var todo []struct {
		k key
		a shortcutAction
	}
	for _, x := range []struct{ name, key, extra string }{{"menu", *menuKey, *picker}, {"repeat", *repeatKey, ""}} {
		if x.key == "" {
			continue
		}
		k, err := parseKey(x.key)
		if err != nil {
			return 2, err
		}
		command := quoteExec(s.exe) + " " + x.name + options
		if x.extra != "" {
			command += " -picker " + quoteExec(x.extra)
		}
		todo = append(todo, struct {
			k key
			a shortcutAction
		}{k, shortcutAction{x.name, command}})
	}
	if len(todo) == 2 && todo[0].k == todo[1].k {
		return 2, errors.New(i18n.Tf("One key for both the menu and repeat: %s", todo[0].k))
	}

	kind := desktopKind(s.current)
	b := s.binder(kind)
	if b == nil {
		var list []binding
		for _, x := range todo {
			list = append(list, binding{x.k, x.a.command})
		}
		s.manual(kind, list...)
		return 1, nil
	}
	// All are checked first, so that a key already taken leaves nothing half done.
	for _, x := range todo {
		if err := b.check(x.k, x.a.name); err != nil {
			return 1, err
		}
	}
	if err := s.installIcon(); err != nil { // only looks, so the keys matter more
		fmt.Fprintln(s.out, "selkit:", err)
	}
	for _, x := range todo {
		if err := b.set(x.k, x.a); err != nil {
			return 1, err
		}
		fmt.Fprintln(s.out, i18n.Tf("%s now runs: %s", x.k.String(), x.a.command))
	}
	return 0, nil
}

// remove takes the keys away, before the program is deleted, for one. The settings stay, as a reinstall
// would want them: they may hold hand-written rules and plugins.
func (s shortcutCmd) remove(args []string) (int, error) {
	fs := newFlags()
	menu := fs.Bool("menu", false, "")
	repeat := fs.Bool("repeat", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2, errors.New(shortcutUsage)
	}
	b := s.binder(desktopKind(s.current))
	if b == nil {
		fmt.Fprintln(s.out, i18n.T("Selkit assigns no keys on this desktop: remove them where you added them."))
		return 1, nil
	}
	all := *menu == *repeat
	var firstErr error
	for _, x := range []struct {
		name string
		on   bool
	}{{"menu", *menu || all}, {"repeat", *repeat || all}} {
		if !x.on {
			continue
		}
		found, err := b.remove(x.name)
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case found:
			fmt.Fprintln(s.out, i18n.Tf("Shortcut removed: %s", x.name))
		default:
			fmt.Fprintln(s.out, i18n.Tf("No shortcut for %s", x.name))
		}
	}
	if firstErr != nil {
		return 1, firstErr
	}
	if all {
		os.Remove(iconFile(s.dataDir))
		fmt.Fprintln(s.out, i18n.Tf("Settings, rules and plugins stay in %s; delete that folder to remove them too.", s.config))
	}
	return 0, nil
}

func (s shortcutCmd) showStatus() (int, error) {
	b := s.binder(desktopKind(s.current))
	if b == nil {
		fmt.Fprintln(s.out, i18n.T("Selkit assigns no keys on this desktop: see what you added in its keyboard settings."))
		return 0, nil
	}
	t := tabwriter.NewWriter(s.out, 0, 0, 3, ' ', 0)
	for _, name := range []string{"menu", "repeat"} {
		st, err := b.status(name)
		if err != nil {
			return 1, err
		}
		keys := i18n.T("no key")
		if len(st.keys) > 0 {
			keys = strings.Join(st.keys, ", ")
		}
		fmt.Fprintf(t, "%s\t%s\t%s\n", name, keys, st.command)
	}
	t.Flush()
	return 0, nil
}

// open starts the keyboard settings of the desktop.
func (s shortcutCmd) open() (int, error) {
	kind := desktopKind(s.current)
	settings := map[string][][]string{
		"kde":   {{"systemsettings", "kcm_keys"}, {"systemsettings5", "kcm_keys"}},
		"gnome": {{"gnome-control-center", "keyboard"}},
		"xfce":  {{"xfce4-keyboard-settings"}},
	}[kind]
	for _, cmd := range settings {
		if _, err := s.d.lookPath(cmd[0]); err == nil {
			return 0, s.d.start(cmd[0], cmd[1:]...)
		}
	}
	return 1, errors.New(i18n.T("No keyboard settings known for this desktop"))
}

// manual says how to do it by hand on a desktop where Selkit cannot.
func (s shortcutCmd) manual(kind string, bindings ...binding) {
	switch kind {
	case "sway", "i3":
		fmt.Fprintln(s.out, i18n.Tf("Add this to the %s configuration:", kind))
	case "hyprland":
		fmt.Fprintln(s.out, i18n.Tf("Add this to the %s configuration:", "Hyprland"))
	default:
		fmt.Fprintln(s.out, i18n.T("Selkit cannot assign keys on this desktop. In its keyboard settings, assign this to a key:"))
	}
	for _, b := range bindings {
		switch kind {
		case "sway", "i3":
			fmt.Fprintf(s.out, "  bindsym %s exec %s\n", b.k.bindsym(), b.command)
		case "hyprland":
			fmt.Fprintf(s.out, "  bind = %s, %s, exec, %s\n", b.k.hyprMods(), b.k.name, b.command)
		default:
			fmt.Fprintf(s.out, "  %s   %s\n", b.k, b.command)
		}
	}
}

func desktopKind(current string) string {
	for _, part := range strings.Split(strings.ToLower(current), ":") {
		switch part {
		case "kde":
			return "kde"
		case "gnome", "unity":
			return "gnome"
		case "xfce":
			return "xfce"
		case "sway", "i3", "hyprland":
			return part
		}
	}
	return ""
}

func (s shortcutCmd) binder(kind string) binder {
	switch kind {
	case "kde":
		return kdeBinder{s.d, filepath.Join(s.dataDir, "applications"), s.exe}
	case "xfce":
		return xfceBinder{s.d}
	case "gnome":
		return gnomeBinder{s.d}
	}
	return nil
}

// keyName is the Qt form of what KDE reports as a number.
func keyName(code int) string {
	k := key{mods: code & (modShift | modCtrl | modAlt | modMeta)}
	plain := code &^ k.mods
	for name, c := range namedKeys {
		if c == plain {
			k.name = name
		}
	}
	switch {
	case k.name != "":
	case plain >= 0x01000030 && plain < 0x01000030+35:
		k.name = "F" + strconv.Itoa(plain-0x01000030+1)
	case plain > 0x20 && plain < 0x7f:
		k.name = string(rune(plain))
	default:
		k.name = fmt.Sprintf("%#x", plain)
	}
	return k.String()
}

// key is a parsed key combination.
type key struct {
	mods int
	name string // as in Qt: "W", "F9", "PageUp"
}

const (
	modShift = 0x02000000
	modCtrl  = 0x04000000
	modAlt   = 0x08000000
	modMeta  = 0x10000000
)

// namedKeys are Qt::Key values.
var namedKeys = map[string]int{
	"Space": 0x20, "Escape": 0x01000000, "Tab": 0x01000001, "Backspace": 0x01000003, "Return": 0x01000004,
	"Insert": 0x01000006, "Delete": 0x01000007, "Pause": 0x01000008, "Print": 0x01000009, "Home": 0x01000010,
	"End": 0x01000011, "Left": 0x01000012, "Up": 0x01000013, "Right": 0x01000014, "Down": 0x01000015,
	"PageUp": 0x01000016, "PageDown": 0x01000017,
}

var functionKey = regexp.MustCompile(`^[Ff]([1-9]|1[0-9]|2[0-9]|3[0-5])$`)

// parseKey reads a combination such as Meta+Shift+W.
func parseKey(s string) (key, error) {
	var k key
	parts := strings.Split(s, "+")
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			k.mods |= modCtrl
		case "alt":
			k.mods |= modAlt
		case "shift":
			k.mods |= modShift
		case "meta", "super", "win":
			k.mods |= modMeta
		default:
			return key{}, errors.New(i18n.Tf("Unknown key: %s", s))
		}
	}
	last := parts[len(parts)-1]
	switch {
	case len(last) == 1 && (last[0] >= '0' && last[0] <= '9' || last[0] >= 'a' && last[0] <= 'z' || last[0] >= 'A' && last[0] <= 'Z'):
		k.name = strings.ToUpper(last)
		if k.mods&^modShift == 0 {
			return key{}, errors.New(i18n.Tf("A letter or digit also needs Ctrl, Alt or Meta, or typing it would run the command: %s", s))
		}
	case functionKey.MatchString(last):
		k.name = strings.ToUpper(last)
	default:
		for name := range namedKeys {
			if strings.EqualFold(name, last) {
				k.name = name
			}
		}
		if k.name == "" {
			return key{}, errors.New(i18n.Tf("Unknown key: %s", s))
		}
	}
	return k, nil
}

// String is the Qt form, which KDE also uses in its files.
func (k key) String() string {
	var b strings.Builder
	for _, m := range []struct {
		bit  int
		name string
	}{{modMeta, "Meta"}, {modCtrl, "Ctrl"}, {modAlt, "Alt"}, {modShift, "Shift"}} {
		if k.mods&m.bit != 0 {
			b.WriteString(m.name + "+")
		}
	}
	return b.String() + k.name
}

// qt is the number KDE takes on D-Bus: a Qt::Key plus the modifiers.
func (k key) qt() int {
	if code, ok := namedKeys[k.name]; ok {
		return k.mods | code
	}
	if k.name[0] == 'F' && len(k.name) > 1 {
		n, _ := strconv.Atoi(k.name[1:])
		return k.mods | (0x01000030 + n - 1)
	}
	return k.mods | int(k.name[0])
}

func (k key) bindsym() string {
	var parts []string
	for _, m := range []struct {
		bit  int
		name string
	}{{modMeta, "Mod4"}, {modCtrl, "Ctrl"}, {modAlt, "Mod1"}, {modShift, "Shift"}} {
		if k.mods&m.bit != 0 {
			parts = append(parts, m.name)
		}
	}
	name := k.name
	if len(name) == 1 {
		name = strings.ToLower(name)
	}
	return strings.Join(append(parts, name), "+")
}

func (k key) hyprMods() string {
	var parts []string
	for _, m := range []struct {
		bit  int
		name string
	}{{modMeta, "SUPER"}, {modCtrl, "CTRL"}, {modAlt, "ALT"}, {modShift, "SHIFT"}} {
		if k.mods&m.bit != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(parts, " ")
}

// quoteExec quotes a word as the Exec key of a .desktop file requires.
func quoteExec(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$`<>~|&;*?#()") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s) + `"`
}
