package translit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/resources"
)

// Set is every available system: the built-in ones plus the user's own.
type Set struct{ systems []*System }

var current atomic.Pointer[Set]

// Init loads the built-in systems and the user's .rules files from userDir (a file named like a built-in one
// replaces it). Broken user files are skipped and reported in the error; the rest still work.
func Init(userDir string) error {
	set, err := Load(resources.Translit, "translit", userDir)
	current.Store(set)
	return err
}

// Default returns the systems loaded by Init, or the built-in ones if Init has not been called.
func Default() *Set {
	if s := current.Load(); s != nil {
		return s
	}
	set, _ := Load(resources.Translit, "translit", "")
	current.CompareAndSwap(nil, set)
	return current.Load()
}

// Load reads *.rules from dir in fsys and then from userDir.
func Load(fsys fs.FS, dir, userDir string) (*Set, error) {
	files := map[string]*file{}
	var errs []error
	read := func(name string, data []byte) {
		f, err := parse(string(data))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		files[strings.TrimSuffix(filepath.Base(name), ".rules")] = f
	}
	if entries, err := fs.ReadDir(fsys, dir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".rules") {
				data, _ := fs.ReadFile(fsys, dir+"/"+e.Name())
				read(e.Name(), data)
			}
		}
	}
	if userDir != "" {
		paths, _ := filepath.Glob(filepath.Join(userDir, "*.rules"))
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			read(p, data)
		}
	}

	set := &Set{}
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		s, err := build(id, files, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.rules: %w", id, err))
			continue
		}
		set.systems = append(set.systems, s)
	}
	return set, errors.Join(errs...)
}

// build assembles a system, merging the rules of its base chain.
func build(id string, files map[string]*file, seen []string) (*System, error) {
	if slices.Contains(seen, id) {
		return nil, fmt.Errorf("base loop: %s", strings.Join(append(seen, id), " → "))
	}
	f, ok := files[id]
	if !ok {
		return nil, fmt.Errorf("base %q not found", id)
	}
	h := f.header
	forward := f.forward
	if b := h["base"]; b != "" {
		if _, err := build(b, files, append(seen, id)); err != nil { // also catches loops before merging
			return nil, err
		}
		forward = merge(forward, files[b].mergedForward(files))
	}

	s := &System{
		ID:           id,
		Lang:         strings.ToLower(h["lang"]),
		names:        map[string]string{},
		silent:       map[rune]bool{},
		foldKatakana: h["fold-katakana"] == "yes",
		spaceScripts: h["space-between-scripts"] == "yes",
		capitalize:   scriptTables(fields(h["capitalize-scripts"])),
		forward:      newIndex(forward),
		after:        newIndex(f.after),
	}
	s.priority, _ = strconv.Atoi(h["priority"])
	for k, v := range h {
		if k == "name" {
			s.names[""] = v
		} else if l, ok := strings.CutPrefix(k, "name."); ok {
			s.names[strings.ToLower(l)] = v
		}
	}
	for _, w := range fields(h["detect"]) {
		s.detect = append(s.detect, []rune(strings.ToLower(w))...)
	}
	for _, w := range fields(h["detect-latin"]) {
		w, _ = unescape(w)
		s.latin = append(s.latin, []rune(strings.ToLower(w))...)
	}
	for _, w := range fields(h["silent"]) {
		for _, r := range w {
			s.silent[r] = true
		}
	}

	mode := h["reverse"]
	if mode == "" && f.hasRev {
		mode = "rules"
	}
	switch mode {
	case "rules":
		s.reverse = newIndex(f.reverse)
	case "auto":
		s.reverse = newIndex(invert(f.forward)) // own rules only: base letters stay foreign to this script
	case "", "no":
	default:
		return nil, fmt.Errorf("reverse must be auto, rules or no, not %q", mode)
	}
	return s, nil
}

// mergedForward is the file's [to-latin] rules with its whole base chain merged in.
func (f *file) mergedForward(files map[string]*file) []rule {
	rules := f.forward
	if b, ok := files[f.header["base"]]; ok && b != f {
		rules = merge(rules, b.mergedForward(files))
	}
	return rules
}

// Systems lists every system, by ID.
func (set *Set) Systems() []*System { return set.systems }

// ByID finds a system.
func (set *Set) ByID(id string) *System {
	for _, s := range set.systems {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// ToLatin returns the systems that can write this text in Latin, the most fitting first: by letters typical
// of the language, then the interface language lang, then priority.
func (set *Set) ToLatin(text, lang string) []*System {
	return set.rank(text, lang, (*System).CanForward)
}

// FromLatin returns the systems that can write this Latin text back, the most fitting first.
func (set *Set) FromLatin(text, lang string) []*System {
	return set.rank(text, lang, (*System).fullReverse)
}

func (set *Set) rank(text, lang string, ok func(*System, string) bool) []*System {
	type scored struct {
		s     *System
		score int
	}
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	lower := strings.ToLower(text)
	var list []scored
	for _, s := range set.systems {
		if !ok(s, text) {
			continue
		}
		score := s.priority
		if s.Lang != "" && s.Lang == base {
			score += 100
		}
		for _, r := range s.detect {
			if strings.ContainsRune(lower, r) {
				score += 1000
			}
		}
		list = append(list, scored{s, score})
	}
	slices.SortStableFunc(list, func(a, b scored) int { return b.score - a.score })
	out := make([]*System, len(list))
	for i, x := range list {
		out[i] = x.s
	}
	return out
}

// fullReverse reports whether the system takes this Latin text: it converts every Latin letter of it and,
// if the system has telltale letters (ISO 9 diacritics), the text contains one.
func (s *System) fullReverse(text string) bool {
	if !s.Reversible() || !s.CanReverse(text) {
		return false
	}
	if len(s.latin) > 0 && !strings.ContainsFunc(strings.ToLower(text), func(r rune) bool { return slices.Contains(s.latin, r) }) {
		return false
	}
	out, _ := s.Reverse(text)
	return !strings.ContainsFunc(out, func(r rune) bool { return unicode.Is(unicode.Latin, r) })
}

// Pick chooses how to convert the text: with the system of this ID, or, for an empty ID, the most fitting one.
// forward means writing in Latin; repeating the result goes back. sys is nil when nothing fits.
func (set *Set) Pick(text, id, lang string) (sys *System, forward bool) {
	if id != "" {
		s := set.ByID(id)
		switch {
		case s == nil:
			return nil, false
		case s.CanForward(text):
			return s, true
		case s.Reversible():
			return s, false
		}
		return nil, false
	}
	if l := set.ToLatin(text, lang); len(l) > 0 {
		return l[0], true
	}
	if l := set.FromLatin(text, lang); len(l) > 0 {
		return l[0], false
	}
	return nil, false
}

// Convert writes the text with the system in the direction Pick chose.
func (s *System) Convert(text string, forward bool) (string, bool) {
	if forward {
		return s.Forward(text)
	}
	return s.Reverse(text)
}

// Label is the menu name for converting with the system: "To Latin" or "From Latin",
// with the system name when it was chosen by hand.
func (s *System) Label(forward, named bool, lang string) string {
	name := i18n.T("From Latin")
	if forward {
		name = i18n.T("To Latin")
	}
	if named {
		name += " (" + s.Name(lang) + ")"
	}
	return name
}
