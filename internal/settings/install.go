//go:build windows

package settings

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unsafe"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/plugin"
)

// The plugin list on the tab is built when the window opens, so a plugin installed here shows up in the menu at
// once, and in the list after the window is opened again. The window waits while a plugin downloads: that is
// rare, and the download has its own time limit.

const (
	mbYesNo        = 0x04
	mbIconQuestion = 0x20
	idYes          = 6
)

func confirm(owner uintptr, s string) bool {
	r, _, _ := procMessageBox.Call(owner, uintptr(unsafe.Pointer(utf16(s))), uintptr(unsafe.Pointer(utf16("Selkit"))), mbYesNo|mbIconQuestion)
	return r == idYes
}

func (w *window) installPlugin(source string) {
	if source == "" {
		messageBox(w.hwnd, i18n.T("Enter a folder, a zip file, a link to one, or github.com/owner/repo."))
		return
	}
	in := plugin.NewInstaller(actions.Dir())
	s, err := in.Fetch(context.Background(), source, "")
	if err != nil {
		messageBox(w.hwnd, i18n.T("Could not install the plugin:")+"\n"+err.Error())
		return
	}
	defer s.Discard()
	if slices.ContainsFunc(actions.Modules(), func(p *plugin.Plugin) bool { return p.ID == s.ID }) {
		messageBox(w.hwnd, i18n.Tf("%s is the name of a built-in module: rename the plugin folder", s.ID))
		return
	}
	if !confirm(w.hwnd, describe(s)+"\n\n"+i18n.T("Install this plugin?")) {
		return
	}
	if err := in.Install(s); err != nil {
		messageBox(w.hwnd, i18n.T("Could not install the plugin:")+"\n"+err.Error())
		return
	}
	messageBox(w.hwnd, i18n.Tf("Installed: %s", s.ID)+"\n"+i18n.T("It is in the menu already; reopen the settings to see it in this list."))
}

func (w *window) updatePlugins() {
	in := plugin.NewInstaller(actions.Dir())
	list, _ := plugin.Load(in.Dir)
	ups, errs := in.Updates(context.Background(), list)
	var problems []string
	for _, err := range errs {
		problems = append(problems, err.Error())
	}
	if len(ups) == 0 {
		msg := i18n.T("No updates")
		if len(problems) > 0 {
			msg += "\n\n" + strings.Join(problems, "\n")
		}
		messageBox(w.hwnd, msg)
		return
	}
	var lines []string
	for _, u := range ups {
		lines = append(lines, fmt.Sprintf("%s   %s → %s", u.ID, or(u.From, "?"), u.To))
	}
	if !confirm(w.hwnd, strings.Join(lines, "\n")+"\n\n"+i18n.T("Update?")) {
		return
	}
	var done []string
	for _, u := range ups {
		s, err := in.FetchUpdate(context.Background(), u.ID)
		if err == nil {
			err = in.Replace(s)
		}
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		done = append(done, i18n.Tf("Updated: %s", u.ID))
	}
	messageBox(w.hwnd, strings.Join(append(done, problems...), "\n"))
}

// describe is what is about to be installed, the command first: that is what will run.
func describe(s *plugin.Staged) string {
	p := s.Plugin
	title := p.Name
	if p.Version != "" {
		title += " " + p.Version
	}
	names := make([]string, len(p.Actions))
	for i, a := range p.Actions {
		names[i] = a.Name
	}
	lines := []string{fmt.Sprintf("%s (%s)", title, s.ID)}
	if p.Description != "" {
		lines = append(lines, p.Description)
	}
	return strings.Join(append(lines,
		"",
		i18n.T("Source:")+" "+s.Origin.Source,
		i18n.T("Runs:")+" "+strings.Join(p.Command, " "),
		i18n.T("Actions:")+" "+strings.Join(names, ", ")), "\n")
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
