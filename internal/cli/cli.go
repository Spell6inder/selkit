package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/i18n"
)

const usage = `Selkit: tools for selected text.

Usage:
  selkit menu [-clipboard] [-type] [-picker NAME]
      Show what can be done with the primary selection (the last selected text), or text from a pipe, and put
      the result into the clipboard and primary. Bind it to a key or mouse button in your desktop.
      -clipboard   take the clipboard instead of the selection
      -type        also type the result into the focused window, replacing the selection
      -picker      menu program: fuzzel, wofi, rofi, bemenu, dmenu, zenity or fzf (default: fuzzel on Wayland, rofi on X11, else the first installed)
  selkit repeat [-clipboard] [-type]
      Run the action last chosen in the menu, without the menu.
  selkit apply ID
      Convert stdin to stdout, e.g.: echo 'Щука' | selkit apply translit
  selkit list | version
      List the action IDs, or show the version.
  selkit plugins [enable ID | disable ID | preview ID on|off]
      List the plugins, or change their settings.
  selkit shortcut [status]
      Show the keys of the menu and of repeat.
  selkit shortcut set [--default] [--menu=KEY] [--repeat=KEY] [--no-type] [--clipboard] [--picker=NAME]
      Assign keys in the desktop. --default is Ctrl+Shift+Alt+X for the menu and Ctrl+Shift+Alt+Z for repeat.
      The result replaces the selection unless --no-type.
  selkit shortcut remove [--menu] [--repeat]
      Take the keys away, both without options; run it before deleting the program.
  selkit shortcut open
      Open the keyboard settings of the desktop.

Needs wl-clipboard (Wayland) or xclip / xsel (X11), and a menu program.
Settings and your own transliteration rules: %s
`

// Run is the command line; it returns the exit code.
func Run(args []string, set actions.Set, configDir string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, usage, configDir)
		return 2
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "list":
		for _, a := range ids(set) {
			fmt.Fprintf(stdout, "%-16s %s\n", a[0], a[1])
		}
		return 0
	case "apply":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: selkit apply ID   (see selkit list)")
			return 2
		}
		in, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "selkit:", err)
			return 1
		}
		// As with menu: the line break echo adds is not part of the text, and comes back after the result.
		text := strings.TrimSuffix(strings.TrimSuffix(string(in), "\n"), "\r")
		r, err := apply(set, args[0], text)
		if err != nil {
			fmt.Fprintln(stderr, "selkit:", err)
			return 1
		}
		io.WriteString(stdout, r.Text)
		if text != string(in) {
			io.WriteString(stdout, "\n")
		}
		return 0
	case "menu", "repeat":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		fs.SetOutput(stderr)
		clipboard := fs.Bool("clipboard", false, "take the clipboard instead of the selection")
		typeIt := fs.Bool("type", false, "type the result into the focused window")
		picker := fs.String("picker", "", "menu program")
		if fs.Parse(args) != nil {
			return 2
		}
		d := newDesktop()
		d.pipe = pipedStdin(stdin, d.terminal, stdout)
		s := session{d, filepath.Join(configDir, "last-action"), set}
		if cmd == "repeat" {
			return s.repeat(!*clipboard, *typeIt)
		}
		return s.menu(!*clipboard, *typeIt, *picker)
	case "plugins":
		return plugins(args, configDir, stdout, stderr)
	case "shortcut":
		return shortcut(args, configDir, stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprintf(stdout, usage, configDir)
		return 0
	}
	fmt.Fprintf(stderr, "selkit: unknown command %q\n\n"+usage, cmd, configDir)
	return 2
}

type session struct {
	d        *desktop
	lastPath string // the ID of the last menu action, for repeat
	set      actions.Set
}

// Length of the result preview to the right of a menu item.
const hintRunes = 40

// repeatMark goes before the action that repeat would run.
const repeatMark = "↻ "

func (s session) menu(primary, typeIt bool, picker string) int {
	text, ok := s.read(primary)
	if !ok {
		return 1
	}
	list := choices(s.set, text)
	if len(list) == 0 {
		s.d.Notify(i18n.T("Nothing to change"), "")
		return 1
	}
	lastID, _ := os.ReadFile(s.lastPath)
	last := strings.TrimSpace(string(lastID))
	lines := make([]menuLine, len(list))
	for i, c := range list {
		switch {
		case c.header:
			lines[i] = menuLine{text: "── " + c.Name + " ──", header: true}
		case c.lazy:
			lines[i] = menuLine{text: c.Name}
		default:
			lines[i] = menuLine{text: c.Name + "   →   " + hint(c.Text)}
		}
		if !c.header && c.ID != "" && c.ID == last {
			lines[i].text = repeatMark + lines[i].text
			lines[i].marked = true
		}
	}
	var i int
	for {
		var err error
		if i, err = s.d.Pick(lines, picker); err != nil {
			s.d.Notify("Selkit", err.Error())
			return 1
		}
		if i < 0 {
			return 0
		}
		if !list[i].header {
			break
		}
		// Most menu programs cannot make a line inactive: choosing a header just shows the menu again.
	}
	os.MkdirAll(filepath.Dir(s.lastPath), 0o755)
	os.WriteFile(s.lastPath, []byte(list[i].ID), 0o644)
	r := list[i].Result
	if list[i].lazy {
		var err error
		if r, err = apply(s.set, r.ID, text); err != nil {
			s.d.Notify(r.Name, err.Error())
			return 1
		}
	}
	return s.deliver(r, typeIt)
}

func (s session) repeat(primary, typeIt bool) int {
	id, err := os.ReadFile(s.lastPath)
	if err != nil {
		s.d.Notify(i18n.T("No action to repeat yet"), "")
		return 1
	}
	text, ok := s.read(primary)
	if !ok {
		return 1
	}
	r, err := apply(s.set, strings.TrimSpace(string(id)), text)
	if err != nil {
		s.d.Notify(err.Error(), "")
		return 1
	}
	return s.deliver(r, typeIt)
}

func (s session) read(primary bool) (string, bool) {
	if s.d.pipe != nil {
		if strings.TrimSpace(s.d.pipe.text) == "" {
			s.d.Notify(i18n.T("Nothing is selected"), "")
			return "", false
		}
		return s.d.pipe.text, true
	}
	text, err := s.d.Read(primary)
	if err != nil {
		s.d.Notify("Selkit", err.Error())
		return "", false
	}
	if strings.TrimSpace(text) == "" {
		s.d.Notify(i18n.T("Nothing is selected"), "")
		return "", false
	}
	return text, true
}

// deliver puts the result into the clipboard and primary and, with typeIt, over the selection.
func (s session) deliver(r actions.Result, typeIt bool) int {
	if p := s.d.pipe; p != nil {
		// stdout is where the result is wanted, so a clipboard that fails is no reason to lose it. Nothing is
		// typed: the text did not come from the focused window.
		io.WriteString(p.out, r.Text)
		if p.newline {
			io.WriteString(p.out, "\n")
		}
		if err := s.d.Write(r.Text); err != nil {
			s.d.Notify("Selkit", err.Error())
		}
		return 0
	}
	if err := s.d.Write(r.Text); err != nil {
		s.d.Notify("Selkit", err.Error())
		return 1
	}
	if typeIt {
		if err := s.d.Type(r.Text); err != nil {
			s.d.Notify(i18n.T("Text not replaced: the result is in the clipboard, paste it with Ctrl+V"), err.Error())
			return 1
		}
		return 0
	}
	s.d.Notify(r.Name, i18n.T("Copied to clipboard")+": "+hint(r.Text))
	return 0
}

// hint is a single-line preview.
func hint(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > hintRunes {
		s = string(r[:hintRunes]) + "…"
	}
	return s
}
