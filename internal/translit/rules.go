package translit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// A .rules file is plain text: a header of "key = value" settings, then sections of "from = to" rules.
//
//	# comment
//	name     = Ukrainian (KMU 2010)
//	name.uk  = Українська (КМУ 2010)
//	base     = iso9            # rules not given here come from iso9.rules
//
//	[to-latin]
//	г  = h
//	^є = ye                    # ^ — only at the start of a word
//	ий$ = yi                   # $ — only at the end of a word
//	зг = zgh                   # several letters at once; the longest match wins
//	ь  =                       # empty: the letter is dropped
//
// Sections: [to-latin] (required), [from-latin] (reverse rules; see "reverse"), [after] (replacements applied
// to each converted run of text, e.g. to merge "ou" into "ō"). Keys are matched case-insensitively; the case
// of the source is carried over to the result. Values may use \uXXXX escapes, e.g. combining accents.
//
// Header keys:
//
//	name, name.<lang>       display name, optionally per interface language
//	lang                    language code of the system ("uk"); preferred when it is the interface language
//	base                    system whose rules fill in the gaps
//	detect                  letters that point to this system when choosing automatically
//	detect-latin            letters (or accents) without which Latin text is not taken back by this system
//	priority                tie-breaker when choosing automatically (higher first, default 0)
//	reverse                 auto (invert [to-latin]), rules ([from-latin], the default if present) or no
//	silent                  characters dropped between two converted letters (apostrophes)
//	fold-katakana           yes: katakana is matched by the hiragana rules
//	space-between-scripts   yes: separate converted runs from neighbouring words of another script
//	capitalize-scripts      scripts whose converted runs start with a capital letter (Katakana)

type rule struct {
	from       []rune // lower case
	to         string
	start, end bool
}

func (r rule) key() string {
	k := string(r.from)
	if r.start {
		k = "^" + k
	}
	if r.end {
		k += "$"
	}
	return k
}

type file struct {
	header  map[string]string
	forward []rule
	reverse []rule
	after   []rule
	hasRev  bool
}

func parse(src string) (*file, error) {
	f := &file{header: map[string]string{}}
	section := ""
	for n, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			section = line[1 : len(line)-1]
			switch section {
			case "to-latin", "after":
			case "from-latin":
				f.hasRev = true
			default:
				return nil, fmt.Errorf("line %d: unknown section [%s]", n+1, section)
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected \"key = value\"", n+1)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("line %d: empty key", n+1)
		}
		if section == "" {
			f.header[strings.ToLower(k)] = v
			continue
		}
		r, err := parseRule(k, v)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		switch section {
		case "to-latin":
			f.forward = append(f.forward, r)
		case "from-latin":
			f.reverse = append(f.reverse, r)
		case "after":
			f.after = append(f.after, r)
		}
	}
	if len(f.forward) == 0 {
		return nil, fmt.Errorf("no [to-latin] rules")
	}
	return f, nil
}

func parseRule(k, v string) (rule, error) {
	var r rule
	if len(k) > 1 && k[0] == '^' {
		r.start, k = true, k[1:]
	}
	if len(k) > 1 && k[len(k)-1] == '$' {
		r.end, k = true, k[:len(k)-1]
	}
	from, err := unescape(k)
	if err != nil {
		return r, err
	}
	if r.to, err = unescape(v); err != nil {
		return r, err
	}
	r.from = []rune(strings.ToLower(from))
	return r, nil
}

// unescape expands \uXXXX sequences.
func unescape(s string) (string, error) {
	if !strings.Contains(s, `\u`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+6 <= len(s) && s[i+1] == 'u' {
			n, err := strconv.ParseUint(s[i+2:i+6], 16, 32)
			if err != nil {
				return "", fmt.Errorf("bad escape %q", s[i:i+6])
			}
			b.WriteRune(rune(n))
			i += 5
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

// index groups rules by their first rune, longest first and context rules before plain ones of the same length.
type index map[rune][]rule

func newIndex(rules []rule) index {
	ix := index{}
	for _, r := range rules {
		if len(r.from) > 0 {
			ix[r.from[0]] = append(ix[r.from[0]], r)
		}
	}
	for _, rs := range ix {
		slices.SortStableFunc(rs, func(a, b rule) int {
			if len(a.from) != len(b.from) {
				return len(b.from) - len(a.from)
			}
			return ctx(b) - ctx(a)
		})
	}
	return ix
}

func ctx(r rule) int {
	n := 0
	if r.start {
		n++
	}
	if r.end {
		n++
	}
	return n
}

// merge returns own rules plus the base rules that own does not redefine (same letters and context).
func merge(own, base []rule) []rule {
	seen := map[string]bool{}
	out := slices.Clone(own)
	for _, r := range own {
		seen[r.key()] = true
	}
	for _, r := range base {
		if !seen[r.key()] {
			out = append(out, r)
		}
	}
	return out
}

// invert turns forward rules into reverse ones; when several letters give the same Latin, the first wins.
func invert(rules []rule) []rule {
	seen := map[string]bool{}
	var out []rule
	for _, r := range rules {
		if r.to == "" {
			continue
		}
		inv := rule{from: []rune(strings.ToLower(r.to)), to: string(r.from), start: r.start, end: r.end}
		if k := inv.key(); !seen[k] {
			seen[k] = true
			out = append(out, inv)
		}
	}
	return out
}

func fields(s string) []string { return strings.Fields(s) }

func scriptTables(names []string) []*unicode.RangeTable {
	var out []*unicode.RangeTable
	for _, n := range names {
		if t, ok := unicode.Scripts[n]; ok {
			out = append(out, t)
		}
	}
	return out
}
