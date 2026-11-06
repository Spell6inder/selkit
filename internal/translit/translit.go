// Package translit writes text in Latin letters (and back) by rules from .rules files: ISO 9 for any Cyrillic,
// official national systems on top of it, kana romanization. Built-in files live in resources/translit;
// the user's own files in the settings folder add systems or replace built-in ones.
package translit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// System is one transliteration system, with its base rules already merged in.
type System struct {
	ID       string
	Lang     string
	names    map[string]string // "" is the default name
	detect   []rune
	latin    []rune // "detect-latin": Latin text is taken as this system's only if it has one of these
	priority int

	forward, reverse, after index
	silent                  map[rune]bool
	foldKatakana            bool
	spaceScripts            bool
	capitalize              []*unicode.RangeTable
}

// Name is the display name for the interface language lang ("uk", "uk-UA").
func (s *System) Name(lang string) string {
	base, _, _ := strings.Cut(lang, "-")
	for _, l := range []string{lang, base, ""} {
		if n, ok := s.names[strings.ToLower(l)]; ok && n != "" {
			return n
		}
	}
	return s.ID
}

// Reversible reports whether the system can write Latin text back in the original script.
func (s *System) Reversible() bool { return len(s.reverse) > 0 }

// CanForward reports whether the text has anything the system converts to Latin.
func (s *System) CanForward(text string) bool { return s.matches(s.forward, text) }

// CanReverse reports whether the text has anything the system converts back.
func (s *System) CanReverse(text string) bool { return s.matches(s.reverse, text) }

func (s *System) matches(ix index, text string) bool {
	for _, r := range text {
		if _, ok := ix[s.fold(unicode.ToLower(r))]; ok && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// Forward writes text in Latin letters; characters without a rule are left as they are.
func (s *System) Forward(text string) (string, bool) { return s.convert(text, s.forward, true) }

// Reverse writes Latin text back in the system's script.
func (s *System) Reverse(text string) (string, bool) { return s.convert(text, s.reverse, false) }

func (s *System) fold(r rune) rune {
	if s.foldKatakana && r >= 0x30A1 && r <= 0x30F6 {
		return r - 0x60
	}
	return r
}

// convert applies rules left to right, longest match first. Converted letters are collected in runs, so that
// [after] rules see a whole word and runs can be separated from neighbouring words of another script.
func (s *System) convert(text string, ix index, forward bool) (string, bool) {
	rs := []rune(text)
	var out, run strings.Builder
	var runScript *unicode.RangeTable
	changed := false
	last := rune(0)  // last rune written to out
	lastRun := false // whether it came from a converted run

	flush := func() {
		if run.Len() == 0 {
			return
		}
		str := run.String()
		if forward {
			str = applyAfter(s.after, str)
			if runScript != nil && slicesContains(s.capitalize, runScript) {
				str = capitalize(str)
			}
		}
		out.WriteString(str)
		if r, _ := utf8.DecodeLastRuneInString(str); str != "" {
			last = r
		}
		lastRun = true
		run.Reset()
		runScript = nil
	}

	for i := 0; i < len(rs); {
		if forward && s.silent[rs[i]] && i > 0 && i+1 < len(rs) && s.converts(ix, rs[i-1]) && s.converts(ix, rs[i+1]) {
			changed = true
			i++
			continue
		}
		to, n := s.match(ix, rs, i)
		if n == 0 {
			flush()
			if forward && s.spaceScripts && unicode.IsLetter(rs[i]) && lastRun {
				out.WriteByte(' ')
			}
			out.WriteRune(rs[i])
			last = rs[i]
			lastRun = false
			i++
			continue
		}
		script := scriptOf(rs[i])
		if forward && s.spaceScripts {
			if run.Len() > 0 && script != runScript {
				flush()
				out.WriteByte(' ')
			} else if run.Len() == 0 && !lastRun && unicode.IsLetter(last) {
				out.WriteByte(' ')
			}
		}
		run.WriteString(withCase(to, rs, i, n))
		runScript = script
		changed = true
		i += n
	}
	flush()
	return out.String(), changed
}

// converts reports whether a rule starts with this rune, i.e. it belongs to the converted script.
func (s *System) converts(ix index, r rune) bool {
	_, ok := ix[s.fold(unicode.ToLower(r))]
	return ok
}

func (s *System) match(ix index, rs []rune, i int) (string, int) {
	for _, r := range ix[s.fold(unicode.ToLower(rs[i]))] {
		n := len(r.from)
		if i+n > len(rs) {
			continue
		}
		ok := true
		for j, c := range r.from {
			if s.fold(unicode.ToLower(rs[i+j])) != c {
				ok = false
				break
			}
		}
		if !ok || (r.start && !s.wordStart(rs, i)) || (r.end && i+n < len(rs) && unicode.IsLetter(rs[i+n])) {
			continue
		}
		return r.to, n
	}
	return "", 0
}

// wordStart reports whether rs[i] begins a word; silent characters (apostrophes) do not break a word.
func (s *System) wordStart(rs []rune, i int) bool {
	for j := i - 1; j >= 0; j-- {
		if s.silent[rs[j]] {
			continue
		}
		return !unicode.IsLetter(rs[j])
	}
	return true
}

// withCase carries the case of the source letters over: ЩУКА → SHCHUKA, Щука → Shchuka, Shchuka → Щука.
func withCase(to string, rs []rune, i, n int) string {
	src := rs[i : i+n]
	if !unicode.IsUpper(src[0]) {
		return to
	}
	allUpper := true
	for _, r := range src {
		allUpper = allUpper && !unicode.IsLower(r)
	}
	if allUpper && (n > 1 || neighbourUpper(rs, i, n)) {
		return strings.ToUpper(to)
	}
	return capitalize(to)
}

// neighbourUpper reports whether the word is written in capitals: the next (or previous) letter is uppercase too.
func neighbourUpper(rs []rune, i, n int) bool {
	if i+n < len(rs) && unicode.IsLetter(rs[i+n]) {
		return unicode.IsUpper(rs[i+n])
	}
	return i > 0 && unicode.IsLetter(rs[i-1]) && unicode.IsUpper(rs[i-1])
}

// applyAfter replaces in a converted run, longest match first, in one pass.
func applyAfter(ix index, s string) string {
	if len(ix) == 0 {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); {
		matched := false
		for _, r := range ix[unicode.ToLower(rs[i])] {
			n := len(r.from)
			if i+n > len(rs) || strings.ToLower(string(rs[i:i+n])) != string(r.from) {
				continue
			}
			if r.end && i+n < len(rs) && unicode.IsLetter(rs[i+n]) {
				continue
			}
			b.WriteString(withCase(r.to, rs, i, n))
			i += n
			matched = true
			break
		}
		if !matched {
			b.WriteRune(rs[i])
			i++
		}
	}
	return b.String()
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

var scripts = []*unicode.RangeTable{unicode.Hiragana, unicode.Katakana, unicode.Han, unicode.Cyrillic, unicode.Latin}

func scriptOf(r rune) *unicode.RangeTable {
	if r == 'ー' {
		return unicode.Katakana // the prolonged sound mark is "Common", but belongs to the katakana word
	}
	for _, t := range scripts {
		if unicode.Is(t, r) {
			return t
		}
	}
	return nil
}

func slicesContains(ts []*unicode.RangeTable, t *unicode.RangeTable) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}
