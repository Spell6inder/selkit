// Package cli is Selkit for desktops without the Windows hooks (Linux): the text menu as a command.
// "selkit menu" takes the primary selection, shows the actions in a menu program (rofi, fuzzel, fzf…)
// and puts the result into the clipboard and primary; "selkit apply" converts stdin to stdout.
// The actions come from internal/actions; the keyboard layout fix is Windows-only.
package cli

import (
	"errors"
	"time"

	"github.com/Spell6inder/selkit/internal/actions"
)

// Time the plugins get to show their result in the menu; slower ones are offered without a preview.
const previewBudget = 500 * time.Millisecond

// choice is a menu line: an action with its result, (lazy) one that runs only when chosen,
// or (header) the name of the group below it.
type choice struct {
	actions.Result
	lazy   bool
	header bool
}

// choices flattens the menu tree into the actions that fit the text, in menu order.
// A submenu becomes a header followed by its actions; one left with no actions is dropped.
func choices(set actions.Set, text string) []choice {
	tree := set.Tree(text)
	preview := set.Preview(tree, text, previewBudget)
	var out []choice
	var walk func([]actions.Entry)
	walk = func(es []actions.Entry) {
		for _, e := range es {
			switch {
			case e.Sub != nil:
				n := len(out)
				out = append(out, choice{Result: actions.Result{Name: e.Name}, header: true})
				walk(e.Sub)
				if len(out) == n+1 {
					out = out[:n]
				}
			case e.ID == "":
			default:
				r, known := preview[e.ID]
				if known && r.Why != "" {
					continue
				}
				if !known {
					r = actions.Result{ID: e.ID, Name: set.Name(e.ID)}
				}
				out = append(out, choice{Result: r, lazy: !known})
			}
		}
	}
	walk(tree)
	return out
}

// apply runs the action and fails with the reason when it does nothing.
func apply(set actions.Set, id, text string) (actions.Result, error) {
	r := set.Eval(id, text)
	if r.Why != "" {
		return r, errors.New(r.Why)
	}
	return r, nil
}

// ids lists every action ID with its name, for "selkit list".
func ids(set actions.Set) [][2]string {
	var list [][2]string
	var walk func([]actions.Entry)
	walk = func(es []actions.Entry) {
		for _, e := range es {
			if e.Sub != nil {
				walk(e.Sub)
			} else if e.ID != "" {
				list = append(list, [2]string{e.ID, set.Name(e.ID)})
			}
		}
	}
	walk(set.All())
	return list
}
