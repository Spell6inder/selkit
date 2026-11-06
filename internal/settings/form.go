package settings

import (
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/plugin"
)

type fieldKind int

const (
	kLabel  fieldKind = iota // plain label next to a field
	kHeader                  // group header
	kNote                    // multi-line note
	kCheck
	kChoice
	kNumber
	kList
	kPlugin  // a plugin: "enabled" and "preview in menu" checkboxes, with a line about it
	kButton  // a push button with its own command ID
	kInstall // a line for where a plugin comes from, with an "Install" button
)

type option struct{ value, label string }

type field struct {
	kind  fieldKind
	label string
	opts  []option
	b     func(c *config.Config) *bool
	s     func(c *config.Config) *string
	n     func(c *config.Config) *int
	l     func(c *config.Config) *[]string
	plug  *plugin.Plugin // kPlugin
	id    int            // kButton
	hwnd  uintptr
	hwnd2 uintptr // kPlugin: the preview checkbox
}

func header(label string) *field { return &field{kind: kHeader, label: label} }
func note(label string) *field   { return &field{kind: kNote, label: label} }
func check(label string, b func(c *config.Config) *bool) *field {
	return &field{kind: kCheck, label: label, b: b}
}
func choice(label string, opts []option, s func(c *config.Config) *string) *field {
	return &field{kind: kChoice, label: label, opts: opts, s: s}
}
func number(label string, n func(c *config.Config) *int) *field {
	return &field{kind: kNumber, label: label, n: n}
}
func list(label string, l func(c *config.Config) *[]string) *field {
	return &field{kind: kList, label: label, l: l}
}
func pluginRow(p *plugin.Plugin) *field  { return &field{kind: kPlugin, label: p.Name, plug: p} }
func button(label string, id int) *field { return &field{kind: kButton, label: label, id: id} }

func modifiers(none string) []option {
	opts := []option{{"ctrl", "Ctrl"}, {"shift", "Shift"}, {"alt", "Alt"}, {"win", "Win"}}
	if none != "" {
		opts = append([]option{{"", none}}, opts...)
	}
	return opts
}

// page is a window tab; about is a special one with no config fields.
type page struct {
	title  string
	fields []*field
	about  bool
	hwnd   uintptr
}

func pages(langs []string, plugs []*plugin.Plugin, plugErr error) []*page {
	langOpts := []option{{"", i18n.T("As in Windows")}}
	for _, l := range langs {
		langOpts = append(langOpts, option{l, i18n.LanguageName(l)})
	}
	return []*page{
		{title: i18n.T("General"), fields: []*field{
			check(i18n.T("Enabled"), func(c *config.Config) *bool { return &c.Enabled }),
			choice(i18n.T("Language (after restart)"), langOpts, func(c *config.Config) *string { return &c.Language }),
			choice(i18n.T("Save selection to primary"), []option{
				{"auto", i18n.T("After every selection")},
				{"hotkey", i18n.T("Only with a combination")},
			}, func(c *config.Config) *string { return &c.Capture }),
			choice(i18n.T("How to read the selection"), []option{
				{"both", i18n.T("UI Automation, then Ctrl+C")},
				{"uia", i18n.T("UI Automation only")},
				{"copy", i18n.T("Ctrl+C only")},
			}, func(c *config.Config) *string { return &c.Mode }),
			check(i18n.T("Catch keyboard selection (Shift+arrows, Ctrl+A)"), func(c *config.Config) *bool { return &c.KeyboardSelection }),
			check(i18n.T("Remove Ctrl+C hack copies from Win+V history"), func(c *config.Config) *bool { return &c.CleanHistory }),
			header(i18n.T("Hint near the cursor")),
			choice(i18n.T("Show"), []option{
				{"off", i18n.T("Never")},
				{"change", i18n.T("When primary changes")},
				{"always", i18n.T("Also after pasting")},
			}, func(c *config.Config) *string { return &c.OSD }),
			number(i18n.T("Duration, ms"), func(c *config.Config) *int { return &c.OSDDurationMs }),
		}},
		{title: i18n.T("Mouse"), fields: []*field{
			header(i18n.T("Middle button with a modifier")),
			choice(i18n.T("Paste primary"), modifiers(i18n.T("No modifier")), func(c *config.Config) *string { return &c.PasteModifier }),
			choice(i18n.T("Save selection (combination mode)"), modifiers(""), func(c *config.Config) *string { return &c.CaptureModifier }),
			choice(i18n.T("Text menu"), modifiers(i18n.T("Off")), func(c *config.Config) *string { return &c.MenuModifier }),
			choice(i18n.T("Repeat last action"), modifiers(i18n.T("Off")), func(c *config.Config) *string { return &c.RepeatModifier }),
			note(i18n.T("Each modifier can do only one thing. Windows itself has no middle-button shortcuts, " +
				"but browsers open links in new tabs with Ctrl and Shift. In programs from the “Paste only into " +
				"input fields” list such a click goes to the program when the cursor is over a link or nothing is " +
				"selected. Alt and Win clash with programs least often.")),
		}},
		{title: i18n.T("Programs"), fields: []*field{
			list(i18n.T("Terminals (paste with Shift+Insert, no Ctrl+C)"), func(c *config.Config) *[]string { return &c.Terminals }),
			list(i18n.T("Disabled in these programs"), func(c *config.Config) *[]string { return &c.Disabled }),
			list(i18n.T("Paste only into input fields"), func(c *config.Config) *[]string { return &c.PasteOnlyInEdit }),
			list(i18n.T("Copy only from input fields"), func(c *config.Config) *[]string { return &c.CopyOnlyInEdit }),
		}},
		{title: i18n.T("Advanced"), fields: []*field{
			header(i18n.T("Timings")),
			number(i18n.T("Delay after selection, ms"), func(c *config.Config) *int { return &c.SelectDelayMs }),
			number(i18n.T("Ctrl+C timeout, ms"), func(c *config.Config) *int { return &c.CopyTimeoutMs }),
			number(i18n.T("Paste timeout, ms"), func(c *config.Config) *int { return &c.PasteTimeoutMs }),
			number(i18n.T("Restore delay, ms"), func(c *config.Config) *int { return &c.RestoreDelayMs }),
			number(i18n.T("UI Automation timeout, ms (after restart)"), func(c *config.Config) *int { return &c.UIATimeoutMs }),
			header(i18n.T("Diagnostics")),
			check(i18n.T("Write the selected text to the log"), func(c *config.Config) *bool { return &c.LogText }),
		}},
		pluginPage(plugs, plugErr),
		{title: i18n.T("About"), about: true},
	}
}

// pluginPage lists the installed plugins; they are read when the window opens.
func pluginPage(plugs []*plugin.Plugin, err error) *page {
	p := &page{title: i18n.T("Plugins")}
	external := 0
	for _, pl := range plugs {
		p.fields = append(p.fields, pluginRow(pl))
		if !pl.Builtin {
			external++
		}
	}
	if external == 0 {
		p.fields = append(p.fields, note(i18n.T("No plugins installed. A plugin is a folder with plugin.json "+
			"in the plugins folder; it gets the selected text and returns the result. See docs/plugins.md.")))
	}
	if err != nil {
		p.fields = append(p.fields, note(i18n.T("Not loaded:")+" "+err.Error()))
	}
	p.fields = append(p.fields,
		&field{kind: kInstall, label: i18n.T("Install from a folder, a zip file or GitHub (github.com/owner/repo):")},
		button(i18n.T("Check for updates"), idUpdates),
		button(i18n.T("Open plugins folder"), idPlugins))
	return p
}
