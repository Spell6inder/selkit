package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"text/tabwriter"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/plugin"
)

const pluginsUsage = `usage: selkit plugins [enable ID | disable ID | preview ID on|off]
       selkit plugins install FOLDER|FILE.zip|URL.zip|github.com/OWNER/REPO [--name=ID] [--yes]
       selkit plugins update [ID] [--yes]
       selkit plugins remove ID [--yes]`

// plugins is "selkit plugins": it lists the plugins, changes one setting in config.json, or installs,
// updates and removes them.
func plugins(args []string, configDir string, stdout, stderr io.Writer) int {
	dir := filepath.Join(configDir, "plugins")
	path := filepath.Join(configDir, "config.json")
	if len(args) > 0 && (args[0] == "install" || args[0] == "update" || args[0] == "remove") {
		m := manage{plugin.NewInstaller(dir), path, stdout, stderr}
		code, err := m.run(args[0], args[1:])
		if err != nil {
			fmt.Fprintln(stderr, "selkit:", err)
		}
		return code
	}
	installed, _ := plugin.Load(dir) // main has already reported the broken ones
	all := actions.Modules()
	for _, p := range installed {
		if !slices.ContainsFunc(all, func(m *plugin.Plugin) bool { return m.ID == p.ID }) { // a module's name: not loaded
			all = append(all, p)
		}
	}
	settings, err := config.ReadPlugins(path)
	if err != nil {
		fmt.Fprintln(stderr, "selkit:", err)
		return 1
	}
	if len(args) == 0 {
		listPlugins(stdout, all, settings)
		if len(all) == len(actions.Modules()) {
			fmt.Fprintln(stdout, i18n.Tf("No plugins in %s", dir))
		}
		return 0
	}

	var id string
	var change func(*config.PluginSettings)
	switch {
	case len(args) == 2 && args[0] == "enable":
		id, change = args[1], func(s *config.PluginSettings) { s.Disabled = false }
	case len(args) == 2 && args[0] == "disable":
		id, change = args[1], func(s *config.PluginSettings) { s.Disabled = true }
	case len(args) == 3 && args[0] == "preview" && (args[2] == "on" || args[2] == "off"):
		on := args[2] == "on"
		id, change = args[1], func(s *config.PluginSettings) { s.Preview = &on }
	default:
		fmt.Fprintln(stderr, pluginsUsage)
		return 2
	}
	at := slices.IndexFunc(all, func(p *plugin.Plugin) bool { return p.ID == id })
	if at < 0 {
		fmt.Fprintln(stderr, "selkit:", i18n.Tf("Unknown plugin: %s", id))
		return 1
	}
	if all[at].Builtin && args[0] == "preview" {
		fmt.Fprintln(stderr, "selkit:", i18n.Tf("%s is built in and always shows a preview", id))
		return 1
	}
	s := settings[id]
	change(&s)
	if err := config.WritePlugin(path, id, s); err != nil {
		fmt.Fprintln(stderr, "selkit:", err)
		return 1
	}
	listPlugins(stdout, all[at:at+1], map[string]config.PluginSettings{id: s})
	return 0
}

func listPlugins(w io.Writer, list []*plugin.Plugin, settings map[string]config.PluginSettings) {
	t := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, p := range list {
		s := settings[p.ID]
		state, preview := i18n.T("enabled"), i18n.T("no preview")
		if s.Disabled {
			state = i18n.T("disabled")
		}
		switch {
		case p.Builtin:
			preview = i18n.T("built-in")
		case p.Preview && s.Preview == nil || s.Preview != nil && *s.Preview:
			preview = i18n.T("preview")
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", p.ID, state, preview, p.Name)
	}
	t.Flush()
}
