package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// desktop talks to the session through the usual command-line tools, so no X11 or Wayland libraries are needed.
type desktop struct {
	wayland  bool
	terminal bool                         // started from a terminal: fzf can be the menu
	lookPath func(string) (string, error) // exec.LookPath; replaced in tests
	run      func(stdin, name string, args ...string) (string, error)
	runMenu  func(stdin, name string, args ...string) (string, error)
	start    func(name string, args ...string) error // does not wait
	hasIcon  bool                                    // the icon is in the icon theme, so a menu can show it
	display  bool                                    // X11 is reachable, through Xwayland on Wayland
	gnome    bool                                    // no layer-shell protocol on Wayland
	pipe     *pipeInput                              // text from stdin, instead of the selection
}

func newDesktop() *desktop {
	// A terminal is what fzf needs; stdin says nothing: a key binding often gives /dev/null, also a character device.
	tty, err := os.Open("/dev/tty")
	if err == nil {
		tty.Close()
	}
	_, iconErr := os.Stat(iconFile(dataHome()))
	return &desktop{
		hasIcon: iconErr == nil,
		display: os.Getenv("DISPLAY") != "",
		gnome:   desktopKind(os.Getenv("XDG_CURRENT_DESKTOP")) == "gnome",

		wayland:  os.Getenv("WAYLAND_DISPLAY") != "",
		terminal: err == nil,
		lookPath: exec.LookPath,
		run:      run,
		runMenu:  runMenu,
		start:    start,
	}
}

func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func run(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	// xclip and wl-copy stay in the background to serve the selection, holding the output pipes open.
	cmd.WaitDelay = 200 * time.Millisecond
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return out.String(), fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return out.String(), fmt.Errorf("%s: %w", name, err)
	}
	return out.String(), nil
}

// runMenu is run for a menu program: its stderr stays on the terminal, because that is where fzf draws.
func runMenu(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

// tool is a command line for one job; the first installed one is used.
type tool struct {
	wayland bool // needs a Wayland session
	args    []string
}

func (d *desktop) find(tools []tool) []string {
	for _, t := range tools {
		if t.wayland && !d.wayland {
			continue
		}
		if _, err := d.lookPath(t.args[0]); err == nil {
			return t.args
		}
	}
	return nil
}

var noClipboardTool = i18n.Mark("No clipboard tool found: install wl-clipboard (Wayland), xclip or xsel")

// Read returns the primary selection, or the clipboard.
func (d *desktop) Read(primary bool) (string, error) {
	sel, xsel := "clipboard", "-b"
	var wl []string
	if primary {
		sel, xsel, wl = "primary", "-p", []string{"--primary"}
	}
	args := d.find([]tool{
		{true, append([]string{"wl-paste", "--no-newline"}, wl...)},
		{false, []string{"xclip", "-o", "-selection", sel}},
		{false, []string{"xsel", "-o", xsel}},
	})
	if args == nil {
		return "", errors.New(i18n.T(noClipboardTool))
	}
	out, err := d.run("", args[0], args[1:]...)
	if err != nil && out == "" {
		// These tools also fail when the selection is empty; only no session at all is worth reporting.
		if msg := strings.ToLower(err.Error()); strings.Contains(msg, "display") || strings.Contains(msg, "connect") {
			return "", err
		}
		return "", nil
	}
	return out, nil
}

// Write puts the text into the clipboard and into primary, so both Ctrl+V and the middle button paste it.
func (d *desktop) Write(text string) error {
	set := func(sel, xsel string, wl ...string) error {
		args := d.find([]tool{
			{true, append([]string{"wl-copy"}, wl...)},
			{false, []string{"xclip", "-i", "-selection", sel}},
			{false, []string{"xsel", "-i", xsel}},
		})
		if args == nil {
			return errors.New(i18n.T(noClipboardTool))
		}
		_, err := d.run(text, args[0], args[1:]...)
		return err
	}
	if err := set("clipboard", "-b"); err != nil {
		return err
	}
	set("primary", "-p", "--primary") // not every compositor has primary; the clipboard is enough
	return nil
}

// Type types the text into the focused window, replacing its selection.
func (d *desktop) Type(text string) error {
	noTool := errors.New(i18n.T("No typing tool found: install wtype (Wayland) or xdotool (X11)"))
	time.Sleep(150 * time.Millisecond) // the window behind the menu gets focus back
	if !d.wayland {
		args := d.find([]tool{{false, []string{"xdotool", "type", "--clearmodifiers", "--"}}})
		if args == nil {
			return noTool
		}
		return d.typeLines(args, text)
	}
	// Each can fail here: KWin has no virtual keyboard for wtype, and ydotool needs its daemon and /dev/uinput.
	// Those two send key positions, not text, so past wtype the result is pasted instead of typed: Cyrillic
	// would be lost without an error. Shift+Insert does not depend on the layout, and in a terminal, where
	// it may paste primary, primary holds the same text. xdotool reaches Wayland windows through Xwayland;
	// the first time, KDE asks the user to allow it.
	err := noTool
	for _, args := range [][]string{
		append([]string{"wtype", "--"}, text),
		{"ydotool", "key", "42:1", "110:1", "110:0", "42:0"},
		{"xdotool", "key", "--clearmodifiers", "shift+Insert"},
	} {
		if _, lookErr := d.lookPath(args[0]); lookErr != nil {
			continue
		}
		if _, err = d.run("", args[0], args[1:]...); err == nil {
			return nil
		}
	}
	return err
}

// typeLines presses Return itself: for a line break, xdotool type sends a key that applications ignore.
func (d *desktop) typeLines(args []string, text string) error {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if i > 0 {
			if _, err := d.run("", args[0], "key", "--clearmodifiers", "Return"); err != nil {
				return err
			}
		}
		if line == "" {
			continue
		}
		if _, err := d.run("", args[0], append(args[1:], line)...); err != nil {
			return err
		}
	}
	return nil
}

// fzf is the menu in a terminal.
var fzf = tool{false, []string{"fzf", "--prompt", "Selkit> ", "--no-sort"}}

// pickers are menu programs that read lines on stdin and print the chosen one.
var pickers = []tool{
	// Without DPI awareness: on a monitor that reports no physical size, fuzzel stays invisible otherwise.
	{true, []string{"fuzzel", "--dmenu", "--dpi-aware=no", "--width=70", "--prompt", "Selkit: "}},
	// Without its cache: wofi would move what was chosen before to the top, breaking the groups,
	// and keep the previews of texts selected earlier.
	{true, []string{"wofi", "--dmenu", "--insensitive", "--prompt", "Selkit", "--cache-file=/dev/null"}},
	{false, []string{"rofi", "-dmenu", "-i", "-p", "Selkit"}},
	{false, []string{"bemenu", "-i", "-p", "Selkit"}},
	{false, []string{"dmenu", "-i", "-p", "Selkit"}},
	{false, []string{"zenity", "--list", "--title=Selkit", "--column=Selkit", "--hide-header", "--width=600", "--height=500"}},
}

// menuLine is a line of a menu; a header names a group of actions and does nothing, and marked is the action
// that repeat would run.
type menuLine struct {
	text   string
	header bool
	marked bool
}

// Pick shows the lines in a menu and returns the index of the chosen one, or -1. Only rofi can make a line
// inactive; with the others a header can be chosen, and the caller has to deal with that.
// name forces a menu program ("rofi"); its usual arguments are added when it is a known one.
func (d *desktop) Pick(lines []menuLine, name string) (int, error) {
	list := d.pickerOrder()
	if d.terminal {
		list = append([]tool{fzf}, list...)
	}
	var args []string
	if name != "" {
		args = []string{name}
		for _, t := range append(list, fzf) {
			if t.args[0] == name {
				args = t.args
				break
			}
		}
	} else if args = d.find(list); args == nil {
		return -1, errors.New(i18n.T("No menu program found: install fuzzel, wofi, rofi, bemenu, dmenu, zenity or fzf"))
	}
	in, args := d.menuInput(lines, args)
	out, err := d.runMenu(in, args[0], args[1:]...)
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return -1, err // could not start it; a non-zero exit is how menu programs report a dismissal
	}
	choice := strings.TrimRight(out, "\r\n")
	if choice == "" {
		return -1, nil // dismissed; menu programs also exit non-zero then
	}
	for i, l := range lines {
		if l.text == choice {
			return i, nil
		}
	}
	if err != nil {
		return -1, err
	}
	return -1, nil // typed something that is not in the list
}

// Notify shows a desktop notification, or writes to stderr without notify-send.
func (d *desktop) Notify(title, body string) {
	if _, err := d.lookPath("notify-send"); err == nil {
		if _, err := d.run("", "notify-send", "--app-name=Selkit", "--icon=selkit", "--expire-time=3000", title, body); err == nil {
			return
		}
	}
	switch {
	case title == "Selkit":
		title = body
	case body != "":
		title += ": " + body
	}
	fmt.Fprintln(os.Stderr, "selkit:", title)
}
