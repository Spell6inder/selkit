package engine

import (
	"log"
	"slices"
	"strings"
	"time"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/clip"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/layout"
	"github.com/Spell6inder/selkit/internal/menu"
	"github.com/Spell6inder/selkit/internal/osd"
	"github.com/Spell6inder/selkit/internal/win"
)

const (
	gestureMenu   = "menu"   // MenuModifier + middle button
	gestureRepeat = "repeat" // RepeatModifier + middle button
)

// How long to wait for the window that got focus back to be ready for input again.
const refocusDelay = 60 * time.Millisecond

// Length of the result preview to the right of a menu item.
const hintRunes = 32

type actionKind int

const (
	actPrimary actionKind = iota
	actLayout
	actID // a portable action from internal/actions
)

// action is what to do with the selection. It is remembered as the "last action", so the result is
// recomputed from the current text each time: "next layout" cycles, Latin goes both ways.
type action struct {
	kind actionKind
	id   string  // actID: action ID
	to   uintptr // actLayout: target layout; 0 means the one after the layout the text was typed in
}

type outcome struct {
	name   string // for the menu, log and tooltip
	text   string
	layout uintptr // layout to switch the window to after replacing
	why    string  // non-empty: the action does nothing here, and why
}

// comboHeld reports whether the menu or repeat modifier is held; called from the hook, so GetAsyncKeyState only.
func (e *Engine) comboHeld() bool {
	cfg := e.cfg.Load()
	if cfg.MenuModifier == "" && cfg.RepeatModifier == "" {
		return false
	}
	held := win.HeldModifier()
	return held != "" && (e.menu != nil && strings.EqualFold(held, cfg.MenuModifier) ||
		strings.EqualFold(held, cfg.RepeatModifier))
}

// grabForAction gets the selection for the menu or repeat; ok=false means there is nothing to work with (the user was already told).
func (e *Engine) grabForAction(cfg *config.Config, gesture string, pt win.Point) (selection, bool) {
	s, ok := e.grabSelection(cfg, gesture, pt)
	if !ok {
		return s, false
	}
	if strings.TrimSpace(s.text) == "" {
		log.Printf("[%s] %-12s ✘ nothing selected (%s, %v)", s.proc, gesture, s.how, s.took)
		e.notify(i18n.T("Nothing is selected"), "")
		return s, false
	}
	return s, true
}

func (e *Engine) onMenu(cfg *config.Config, pt win.Point) {
	t0 := time.Now()
	s, ok := e.grabForAction(cfg, gestureMenu, pt)
	if !ok {
		return
	}
	ls := layout.Installed()
	set := loadActions(cfg)
	actions, done, items := e.menuItems(set, s, ls)
	id, active := e.menu.Show(items, win.CursorPos())
	// The menu took focus; give it back to the window with the selection (also when the menu was simply dismissed).
	win.ForceForeground(s.fg)
	if !active {
		log.Printf("[%s] menu         menu did not get focus; closes only via Esc or a selection", s.proc)
	}
	if id <= 0 || id > len(actions) {
		log.Printf("[%s] menu         closed without a selection", s.proc)
		return
	}
	time.Sleep(refocusDelay)
	a := actions[id-1]
	o, ok := done[id-1]
	if !ok { // a plugin without preview, or too slow for it
		o = evaluate(set, a, s, ls)
	}
	e.perform(cfg, gestureMenu, s, a, o, pt, t0)
}

// onRepeat runs the last menu action without showing the menu.
func (e *Engine) onRepeat(cfg *config.Config, pt win.Point) {
	t0 := time.Now()
	if e.last == nil {
		log.Printf("[%s] repeat       ✘ no menu action yet", win.ProcessAt(pt))
		e.notify(i18n.T("No action to repeat yet"), "")
		return
	}
	s, ok := e.grabForAction(cfg, gestureRepeat, pt)
	if !ok {
		return
	}
	a := *e.last
	e.perform(cfg, gestureRepeat, s, a, evaluate(loadActions(cfg), a, s, layout.Installed()), pt, t0)
}

// perform runs the action: saves to primary, replaces the selection or, if the field is not editable, puts it on the clipboard.
func (e *Engine) perform(cfg *config.Config, gesture string, s selection, a action, o outcome, pt win.Point, t0 time.Time) {
	if o.why != "" {
		log.Printf("[%s] %-12s ✘ %s: %s", s.proc, gesture, o.name, o.why)
		e.notify(o.why, "")
		return
	}
	if a.kind == actPrimary {
		e.setPrimary(s.text)
		log.Printf("[%s] %-12s ✔ to primary %s (%s, %v)", s.proc, gesture, describeText(cfg, s.text), s.how, s.took)
		e.showOSD(cfg, "Primary", s.text, false)
		return
	}
	e.last = &a

	// Pasting replaces the selection only where it is editable; otherwise the result goes to the regular clipboard.
	editable := !cfg.IsTerminal(s.proc) &&
		(s.focusedEdit || !cfg.PasteNeedsEdit(s.proc) || e.uia.EditAt(pt))
	if !editable {
		clip.Set([]clip.Item{clip.TextItem(o.text)}, false)
		log.Printf("[%s] %-12s ✔ %s -> to clipboard: field is not editable (%s)", s.proc, gesture, o.name, describeText(cfg, o.text))
		e.notify(i18n.T("Copied to clipboard"), o.text)
		return
	}

	note, ok := e.paste(cfg, o.text, s.proc, false)
	if !ok {
		log.Printf("[%s] %-12s ✘ %s: clipboard busy", s.proc, gesture, o.name)
		return
	}
	if o.layout != 0 {
		layout.Activate(s.fg, o.layout)
	}
	log.Printf("[%s] %-12s ✔ %s -> replaced %s (%s; %s, %v)", s.proc, gesture, o.name, describeText(cfg, o.text),
		s.how, note, time.Since(t0).Round(time.Millisecond))
}

// notify is the response to an explicit user action, so it is shown regardless of the osd setting.
func (e *Engine) notify(action, text string) {
	if e.osd == nil {
		return
	}
	title := action
	if text != "" {
		title = osd.Title(action, text)
	}
	e.osd.Show(title, osd.Preview(text), win.CursorPos(), e.cfg.Load().OSDDuration())
}

// loadActions reads the plugins for this menu; broken ones are only logged.
func loadActions(cfg *config.Config) actions.Set {
	set, err := actions.Load(cfg)
	if err != nil {
		log.Printf("plugins: %v", err)
	}
	return set
}

func evaluate(set actions.Set, a action, s selection, ls []*layout.Layout) outcome {
	switch a.kind {
	case actPrimary:
		return outcome{name: i18n.T("To primary"), text: s.text}
	case actLayout:
		return evalLayout(a, s, ls)
	}
	return fromResult(set.Eval(a.id, s.text))
}

func fromResult(r actions.Result) outcome {
	return outcome{name: r.Name, text: r.Text, why: r.Why}
}
func result(name, in, out string, ok bool) outcome {
	o := outcome{name: name, text: out}
	switch {
	case !ok:
		o.why = i18n.T("Not applicable to this text")
	case out == in:
		o.why = i18n.T("Nothing to change")
	}
	return o
}

// evalLayout converts from the layout the text appears to be typed in to the next one (a.to == 0) or a specific one.
func evalLayout(a action, s selection, ls []*layout.Layout) outcome {
	name := i18n.T("Keyboard layout")
	if len(ls) < 2 {
		return outcome{name: name, why: i18n.T("Only one keyboard layout is installed")}
	}
	from := layout.Detect(ls, s.text, layout.Current(s.fg))
	if from < 0 {
		return outcome{name: name, why: i18n.T("The text does not look typed in any installed layout")}
	}
	to := (from + 1) % len(ls)
	if a.to != 0 {
		if to = slices.IndexFunc(ls, func(l *layout.Layout) bool { return l.HKL == a.to }); to < 0 {
			return outcome{name: name, why: i18n.T("That keyboard layout is no longer installed")}
		}
		if to == from {
			return outcome{name: i18n.Tf("Layout → %s", ls[to].Name), why: i18n.Tf("The text is already in %s", ls[to].Name)}
		}
	}
	o := result(i18n.Tf("Layout %s → %s", ls[from].Name, ls[to].Name), s.text, layout.Convert(s.text, ls[from], ls[to]), true)
	o.layout = ls[to].HKL
	return o
}

// Time the plugins get to show their result in the menu; slower ones are offered without a preview.
const previewBudget = 300 * time.Millisecond

// menuItems builds the menu for the selection; an item's ID is the action's number in actions, starting from 1.
// done holds the results already computed for the preview, by the same index.
func (e *Engine) menuItems(set actions.Set, s selection, ls []*layout.Layout) ([]action, map[int]outcome, []menu.Item) {
	var list []action
	done := map[int]outcome{}
	repeatItem := false // add is making the "Repeat" item, which gets no check mark
	// add adds an item for an action with its result; known=false means it was not tried (shown as available).
	add := func(a action, label string, o outcome, known bool) menu.Item {
		if label == "" {
			label = o.name
		}
		// The action repeat would run, where it stands in the menu; the "Repeat" item above shows it by name.
		it := menu.Item{Label: label, Checked: e.last != nil && a == *e.last && !repeatItem}
		if known && o.why != "" {
			return it // not applicable or changes nothing: greyed out
		}
		list = append(list, a)
		it.ID = len(list)
		if known {
			done[it.ID-1] = o
			if a.kind != actPrimary {
				it.Hint = hint(o.text)
			}
		}
		return it
	}
	local := func(a action, label string) menu.Item { return add(a, label, evaluate(set, a, s, ls), true) }

	tree := set.Tree(s.text)
	preview := set.Preview(tree, s.text, previewBudget)
	// previewed is the result of a portable action as far as it is known; unknown ones are not tried in advance.
	previewed := func(id string) (outcome, bool) {
		if r, ok := preview[id]; ok {
			return fromResult(r), true
		}
		if !inTree(tree, id) { // not in this menu (a system that does not fit, a plugin that is gone)
			return fromResult(set.Eval(id, s.text)), true
		}
		return outcome{name: set.Name(id)}, false
	}
	portable := func(en actions.Entry) menu.Item {
		o, known := previewed(en.ID)
		it := add(action{kind: actID, id: en.ID}, en.Name, o, known)
		if known && o.why != "" && strings.HasPrefix(en.ID, "plugin:") {
			it.Hint = hint(o.why) // the plugin's own explanation, since its item is greyed out
		}
		return it
	}

	items := []menu.Item{{Label: osd.Title(i18n.T("Selected"), s.text), Hint: hint(s.text)}, {}}
	if e.last != nil {
		a := *e.last
		var o outcome
		known := true
		if a.kind == actID {
			o, known = previewed(a.id)
		} else {
			o = evaluate(set, a, s, ls)
		}
		repeatItem = true
		it := add(a, i18n.Tf("Repeat: %s", o.name), o, known)
		repeatItem = false
		if o.why != "" {
			it.Hint = o.why
		}
		items = append(items, it)
	}
	items = append(items, local(action{kind: actPrimary}, ""), menu.Item{})

	items = append(items, local(action{kind: actLayout}, ""))
	if len(ls) > 2 { // with two layouts the only other one is already the "next" item
		// All other layouts, starting from the one after the layout the text was typed in.
		from := max(layout.Detect(ls, s.text, layout.Current(s.fg)), 0)
		sub := make([]menu.Item, 0, len(ls)-1)
		for i := 1; i < len(ls); i++ {
			l := ls[(from+i)%len(ls)]
			sub = append(sub, local(action{kind: actLayout, to: l.HKL}, l.Name))
		}
		items = append(items, menu.Item{Label: i18n.T("Layout to"), Sub: sub})
	}

	var build func([]actions.Entry) []menu.Item
	build = func(es []actions.Entry) []menu.Item {
		out := make([]menu.Item, 0, len(es))
		for _, en := range es {
			switch {
			case en.Sub != nil:
				out = append(out, menu.Item{Label: en.Name, Sub: build(en.Sub)})
			case en.ID == "":
				out = append(out, menu.Item{})
			default:
				out = append(out, portable(en))
			}
		}
		return out
	}
	return list, done, append(items, build(tree)...)
}

func inTree(es []actions.Entry, id string) bool {
	for _, e := range es {
		if e.ID == id || inTree(e.Sub, id) {
			return true
		}
	}
	return false
}

// hint is a single-line preview for the menu (& is a mnemonic in Windows menus, so it is doubled).
func hint(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > hintRunes {
		s = string(r[:hintRunes]) + "…"
	}
	return strings.ReplaceAll(s, "&", "&&")
}
