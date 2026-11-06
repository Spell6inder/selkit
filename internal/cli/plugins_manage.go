package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/plugin"
)

// manage installs, updates and removes plugins. A plugin is a program that gets the selected text, so each of these
// shows what it is and asks first; --yes is the answer for scripts.
type manage struct {
	in     plugin.Installer
	config string
	out    io.Writer
	errOut io.Writer
}

func (m manage) run(cmd string, args []string) (int, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "")
	name := fs.String("name", "", "")
	// Flags may follow the source, as people write them.
	var rest []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return 2, errors.New(pluginsUsage)
		}
		if fs.NArg() == 0 {
			break
		}
		rest, args = append(rest, fs.Arg(0)), fs.Args()[1:]
	}
	ctx := context.Background()
	switch {
	case cmd == "install" && len(rest) == 1:
		return m.install(ctx, rest[0], *name, *yes)
	case cmd == "update" && len(rest) <= 1 && *name == "":
		return m.update(ctx, rest, *yes)
	case cmd == "remove" && len(rest) == 1 && *name == "":
		return m.remove(rest[0], *yes)
	}
	return 2, errors.New(pluginsUsage)
}

func (m manage) install(ctx context.Context, source, name string, yes bool) (int, error) {
	s, err := m.in.Fetch(ctx, source, name)
	if err != nil {
		return 1, err
	}
	defer s.Discard()
	if slices.ContainsFunc(actions.Modules(), func(p *plugin.Plugin) bool { return p.ID == s.ID }) {
		return 1, errors.New(i18n.Tf("%s is the name of a built-in module: choose another with --name", s.ID))
	}
	m.describe(s)
	if !yes && !m.ask(i18n.T("Install this plugin?")) {
		return 1, nil
	}
	if err := m.in.Install(s); err != nil {
		return 1, err
	}
	fmt.Fprintln(m.out, i18n.Tf("Installed: %s", s.ID))
	return 0, nil
}

func (m manage) update(ctx context.Context, only []string, yes bool) (int, error) {
	list, _ := plugin.Load(m.in.Dir)
	if len(only) == 1 {
		list = slices.DeleteFunc(list, func(p *plugin.Plugin) bool { return p.ID != only[0] })
		if len(list) == 0 {
			return 1, errors.New(i18n.Tf("Unknown plugin: %s", only[0]))
		}
	}
	ups, errs := m.in.Updates(ctx, list)
	for _, err := range errs {
		fmt.Fprintln(m.errOut, "selkit:", err)
	}
	if len(ups) == 0 {
		fmt.Fprintln(m.out, i18n.T("No updates"))
		if len(errs) > 0 {
			return 1, nil
		}
		return 0, nil
	}
	for _, u := range ups {
		fmt.Fprintf(m.out, "%s   %s → %s\n", u.ID, or(u.From, "?"), u.To)
	}
	if !yes && !m.ask(i18n.T("Update?")) {
		return 1, nil
	}
	code := 0
	for _, u := range ups {
		s, err := m.in.FetchUpdate(ctx, u.ID)
		if err == nil {
			err = m.in.Replace(s)
		}
		if err != nil {
			fmt.Fprintln(m.errOut, "selkit:", err)
			code = 1
			continue
		}
		fmt.Fprintln(m.out, i18n.Tf("Updated: %s", u.ID))
	}
	return code, nil
}

func (m manage) remove(id string, yes bool) (int, error) {
	if !yes && !m.ask(i18n.Tf("Remove the plugin %s?", id)) {
		return 1, nil
	}
	if err := m.in.Remove(id); err != nil {
		return 1, err
	}
	// Its settings go with it, so that a plugin installed later under the same name starts afresh.
	config.WritePlugin(m.config, id, config.PluginSettings{})
	fmt.Fprintln(m.out, i18n.Tf("Removed: %s", id))
	return 0, nil
}

// describe shows what is about to be installed, the command first: that is what will run.
func (m manage) describe(s *plugin.Staged) {
	p := s.Plugin
	title := p.Name
	if p.Version != "" {
		title += " " + p.Version
	}
	fmt.Fprintf(m.out, "%s (%s)\n", title, s.ID)
	if p.Description != "" {
		fmt.Fprintln(m.out, "  "+p.Description)
	}
	fmt.Fprintln(m.out, "  "+i18n.T("Source:")+" "+s.Origin.Source)
	fmt.Fprintln(m.out, "  "+i18n.T("Runs:")+" "+strings.Join(p.Command, " "))
	names := make([]string, len(p.Actions))
	for i, a := range p.Actions {
		names[i] = a.Name
	}
	fmt.Fprintln(m.out, "  "+i18n.T("Actions:")+" "+strings.Join(names, ", "))
}

// openTTY is the terminal the command runs in; replaced in tests.
var openTTY = func() (io.ReadWriteCloser, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

// ask reads y from the terminal itself, as stdin may be a pipe or /dev/null; anything else, or no terminal, is no.
func (m manage) ask(question string) bool {
	tty, err := openTTY()
	if err != nil {
		fmt.Fprintln(m.errOut, i18n.T("Not asked without a terminal: add --yes"))
		return false
	}
	defer tty.Close()
	fmt.Fprint(tty, question+" [y/N] ")
	answer, _ := bufio.NewReader(tty).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes" || answer == "т" || answer == "так"
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
