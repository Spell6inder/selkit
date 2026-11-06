// Package actions is the portable part of the text menu: transliteration, the textops operations and
// plugins, by stable IDs. The Windows menu (internal/engine) adds the keyboard layout and primary to it;
// the Linux command (internal/cli) shows it as is.
//
// IDs: "translit" (the most fitting system), "translit:<system>", textops IDs ("snake"),
// "plugin:<plugin>/<action>".
package actions

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/plugin"
	"github.com/Spell6inder/selkit/internal/textops"
	"github.com/Spell6inder/selkit/internal/translit"
)

const (
	translitID   = "translit"
	pluginPrefix = "plugin:"
)

// Entry is a menu item: an action (ID), a submenu (Sub) or, with neither, a separator.
type Entry struct {
	ID   string
	Name string // for an action, empty means the name of its result ("To Latin" or "From Latin")
	Sub  []Entry
	Lazy bool // a plugin without preview: offered without trying, run only when chosen
}

// Result is an action applied to the text; Why says why it does nothing.
type Result struct {
	ID, Name, Text, Why string
}

// Set is what the menu offers: the built-in actions plus the enabled plugins.
type Set struct {
	Lang    string
	Plugins []*plugin.Plugin
	preview map[string]bool // plugin ID → run while the menu opens
	off     map[string]bool // built-in modules turned off
}

// Built-in modules: groups of actions that are part of the program, yet shown and turned off like plugins,
// under the same settings. Their IDs are taken: a plugin folder with one of these names is not loaded.
var modules = []struct{ id, name, description string }{
	{translitID, i18n.Mark("Transliteration"), i18n.Mark("Latin script and back, by several systems")},
	{"encoding", i18n.Mark("Encoding"), i18n.Mark("URL and Base64: encode and decode")},
	{"quotes", i18n.Mark("Quotes"), i18n.Mark("Put the text in quotes or take them off")},
}

// Modules lists the built-in modules as plugins, for the settings: they run in the program and always show
// a preview, so only turning them off is up to the user.
func Modules() []*plugin.Plugin {
	list := make([]*plugin.Plugin, len(modules))
	for i, m := range modules {
		list[i] = &plugin.Plugin{ID: m.id, Name: i18n.T(m.name), Description: i18n.T(m.description), Builtin: true, Preview: true}
	}
	return list
}

func isModule(id string) bool {
	for _, m := range modules {
		if m.id == id {
			return true
		}
	}
	return false
}

// moduleOf is the built-in module an action belongs to, or "".
func moduleOf(id string) string {
	if id == translitID || strings.HasPrefix(id, translitID+":") {
		return translitID
	}
	for _, g := range textops.Groups {
		for _, op := range g.Ops {
			if op.ID == id {
				return g.Module
			}
		}
	}
	return ""
}

// Dir is where plugins live: one folder per plugin.
func Dir() string { return filepath.Join(config.Dir(), "plugins") }

// Load reads the plugins (it is cheap, so it is done each time the menu opens) and applies the settings.
// Broken plugins are skipped; the error lists them.
func Load(cfg *config.Config) (Set, error) { return LoadFrom(Dir(), cfg) }

// LoadFrom is Load with plugins from dir.
func LoadFrom(dir string, cfg *config.Config) (Set, error) {
	all, err := plugin.Load(dir)
	s := Set{Lang: i18n.Lang(), preview: map[string]bool{}, off: map[string]bool{}}
	for _, m := range modules {
		s.off[m.id] = cfg.Plugin(m.id).Disabled
	}
	for _, p := range all {
		if isModule(p.ID) {
			err = errors.Join(err, fmt.Errorf("%s: %s", p.ID, i18n.T("the name of a built-in module")))
			continue
		}
		ps := cfg.Plugin(p.ID)
		if ps.Disabled {
			continue
		}
		s.Plugins = append(s.Plugins, p)
		s.preview[p.ID] = p.Preview
		if ps.Preview != nil {
			s.preview[p.ID] = *ps.Preview
		}
	}
	return s, err
}

// Tree lists the actions for the text, in menu order. Actions that do not fit are still listed; Preview tells them apart.
func (s Set) Tree(text string) []Entry {
	set := translit.Default()
	label, systems := i18n.T("Latin by"), set.ToLatin(text, s.Lang)
	if len(systems) == 0 {
		label, systems = i18n.T("From Latin by"), set.FromLatin(text, s.Lang)
	}
	return s.tree(label, systems)
}

// All lists every action, with all transliteration systems.
func (s Set) All() []Entry { return s.tree(i18n.T("Latin by"), translit.Default().Systems()) }

func (s Set) tree(label string, systems []*translit.System) []Entry {
	var tree []Entry
	if !s.off[translitID] {
		tree = append(tree, Entry{ID: translitID})
		if len(systems) > 1 {
			sub := make([]Entry, 0, len(systems))
			for _, sys := range systems {
				sub = append(sub, Entry{ID: translitID + ":" + sys.ID, Name: sys.Name(s.Lang)})
			}
			tree = append(tree, Entry{Name: label, Sub: sub})
		}
		tree = append(tree, Entry{})
	}
	for _, g := range textops.Groups {
		if g.Module != "" && s.off[g.Module] {
			continue
		}
		sub := make([]Entry, 0, len(g.Ops))
		for _, op := range g.Ops {
			if op.Apply == nil {
				sub = append(sub, Entry{})
			} else {
				sub = append(sub, Entry{ID: op.ID, Name: i18n.T(op.Name)})
			}
		}
		tree = append(tree, Entry{Name: i18n.T(g.Name), Sub: sub})
	}
	if len(s.Plugins) > 0 {
		tree = append(tree, Entry{})
	}
	for _, p := range s.Plugins {
		var items []Entry
		for _, a := range p.Actions {
			items = append(items, Entry{ID: pluginPrefix + p.ID + "/" + a.ID, Name: a.Name, Lazy: !s.preview[p.ID]})
		}
		if len(items) == 1 { // a one-action plugin needs no submenu
			tree = append(tree, items[0])
		} else {
			tree = append(tree, Entry{Name: p.Name, Sub: items})
		}
	}
	return tree
}

// Name is the menu name of the action, without running it.
func (s Set) Name(id string) string {
	if id == translitID {
		return i18n.T("To Latin") + " / " + i18n.T("From Latin")
	}
	if sysID, ok := strings.CutPrefix(id, translitID+":"); ok {
		if sys := translit.Default().ByID(sysID); sys != nil {
			return sys.Label(true, true, s.Lang)
		}
	}
	if p, a := s.plugin(id); a != nil {
		if len(p.Actions) > 1 {
			return p.Name + ": " + a.Name
		}
		return a.Name
	}
	if r := s.Eval(id, ""); r.Name != "" {
		return r.Name
	}
	return id
}

// Eval runs the action on the text; plugins get their full timeout.
func (s Set) Eval(id, text string) Result {
	return s.eval(context.Background(), id, text, false)
}

func (s Set) eval(ctx context.Context, id, text string, preview bool) Result {
	if m := moduleOf(id); m != "" && s.off[m] {
		return Result{ID: id, Name: id, Why: i18n.T("The plugin is not installed or is turned off")}
	}
	if sysID, ok := strings.CutPrefix(id, translitID); ok && (sysID == "" || sysID[0] == ':') {
		sysID = strings.TrimPrefix(sysID, ":")
		set := translit.Default()
		if sysID != "" && set.ByID(sysID) == nil {
			return Result{ID: id, Name: id, Why: i18n.T("Unknown action")}
		}
		sys, forward := set.Pick(text, sysID, s.Lang)
		if sys == nil {
			return Result{ID: id, Name: i18n.T("To Latin"), Why: i18n.T("Not applicable to this text")}
		}
		out, ok := sys.Convert(text, forward)
		return result(id, sys.Label(forward, sysID != "", s.Lang), text, out, ok)
	}
	if strings.HasPrefix(id, pluginPrefix) {
		p, a := s.plugin(id)
		if a == nil {
			return Result{ID: id, Name: id, Why: i18n.T("The plugin is not installed or is turned off")}
		}
		name := s.Name(id)
		out, err := p.Run(ctx, a.ID, text, s.Lang, preview)
		if err != nil {
			return Result{ID: id, Name: name, Why: err.Error()}
		}
		return result(id, name, text, out, true)
	}
	op, ok := textops.Find(id)
	if !ok {
		return Result{ID: id, Name: id, Why: i18n.T("Unknown action")}
	}
	out, ok := op.Apply(text)
	return result(id, i18n.T(op.Name), text, out, ok)
}

func (s Set) plugin(id string) (*plugin.Plugin, *plugin.Action) {
	rest, ok := strings.CutPrefix(id, pluginPrefix)
	if !ok {
		return nil, nil
	}
	pid, aid, _ := strings.Cut(rest, "/")
	for _, p := range s.Plugins {
		if p.ID != pid {
			continue
		}
		for i := range p.Actions {
			if p.Actions[i].ID == aid {
				return p, &p.Actions[i]
			}
		}
	}
	return nil, nil
}

func result(id, name, in, out string, ok bool) Result {
	r := Result{ID: id, Name: name, Text: out}
	switch {
	case !ok:
		r.Why = i18n.T("Not applicable to this text")
	case out == in:
		r.Why = i18n.T("Nothing to change")
	}
	return r
}

// Preview applies every action of the tree that is not lazy. Plugins run in parallel, and those that do not
// answer within budget are left out: the menu offers them without a preview.
func (s Set) Preview(tree []Entry, text string, budget time.Duration) map[string]Result {
	out := map[string]Result{}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	var walk func([]Entry)
	walk = func(es []Entry) {
		for _, e := range es {
			switch {
			case e.Sub != nil:
				walk(e.Sub)
			case e.ID == "" || e.Lazy:
			case strings.HasPrefix(e.ID, pluginPrefix):
				wg.Add(1)
				go func(id string) {
					defer wg.Done()
					r := s.eval(ctx, id, text, true)
					if ctx.Err() != nil { // too late, or cut off by the budget
						return
					}
					mu.Lock()
					out[id] = r
					mu.Unlock()
				}(e.ID)
			default:
				r := s.eval(ctx, e.ID, text, true)
				mu.Lock()
				out[e.ID] = r
				mu.Unlock()
			}
		}
	}
	walk(tree)
	wg.Wait()
	return out
}
