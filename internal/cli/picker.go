package cli

import (
	"slices"
	"strconv"
	"strings"
)

// pickerOrder is which menu program to try first. On Wayland that is fuzzel: rofi does not scroll with the wheel
// there, at least on KDE. On X11 fuzzel cannot run, and rofi is the one that can show the icon, make headers
// inactive and highlight a line. GNOME has no layer-shell protocol, without which fuzzel and wofi cannot show
// a menu, so there rofi goes through Xwayland.
func (d *desktop) pickerOrder() []tool {
	if d.wayland && !d.gnome {
		return pickers
	}
	var first, rest []tool
	for _, t := range pickers {
		switch t.args[0] {
		case "rofi":
			if !d.wayland || d.display {
				first = append(first, t)
			}
		case "fuzzel", "wofi":
			if !d.gnome {
				rest = append(rest, t)
			}
		default:
			rest = append(rest, t)
		}
	}
	return append(first, rest...)
}

// rofiLogo puts the icon at the start of the search line, before the prompt. The other widgets are rofi's
// own, so the line looks as usual otherwise; a theme of the user's own that lays out the line differently
// gets this layout instead.
const rofiLogo = `inputbar { children: [ icon-selkit, prompt, textbox-prompt-colon, entry, num-filtered-rows, ` +
	`textbox-num-sep, num-rows, case-indicator ]; } ` +
	`icon-selkit { expand: false; filename: "selkit"; size: 1.4em; margin: 0 0.4em 0 0; vertical-align: 0.5; }`

// menuInput is what goes to the menu program for the lines, and the arguments it needs for them. Only rofi can
// show the icon in its search line, make a header inactive and highlight a line; the rows stay without icons.
func (d *desktop) menuInput(lines []menuLine, args []string) (string, []string) {
	args = slices.Clone(args)
	rofi := args[0] == "rofi"
	if rofi && d.hasIcon {
		args = append(args, "-theme-str", rofiLogo)
	}
	var in strings.Builder
	for i, l := range lines {
		in.WriteString(l.text)
		if rofi && l.header {
			in.WriteString("\x00nonselectable\x1ftrue")
		}
		in.WriteByte('\n')
		if rofi && l.marked {
			args = append(args, "-a", strconv.Itoa(i))
		}
	}
	return in.String(), args
}
