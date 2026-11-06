// Package textops transforms selected text for the context menu: case,
// whitespace and lines, encodings. Pure functions only, no WinAPI.
package textops

import (
	"encoding/base64"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Spell6inder/selkit/internal/i18n"
)

// Op is a single transformation. ID is stable (the last action is remembered by it), Name is the menu label
// in English, doubling as the translation key. Apply returns ok=false if the operation is not applicable to this text
// (for example, it is not Base64). An Op without Apply is a menu separator.
type Op struct {
	ID, Name string
	Apply    func(string) (string, bool)
}

// Group is a submenu. Module is the built-in module it belongs to, which can be turned off like a plugin;
// an empty Module means it is always there.
type Group struct {
	Name   string
	Ops    []Op
	Module string
}

// Groups are the context menu submenus (transliteration and layout are separate items, not here).
var Groups = []Group{
	{i18n.Mark("Case"), []Op{
		// Splits an identifier back into words, keeps letter case, so any case below can be applied next.
		{"words", i18n.Mark("separate words"), perLine(joinWords(" ", func(s string) string { return s }))},
		{"camel", "camelCase", perLine(camel)},
		{"pascal", "PascalCase", perLine(pascal)},
		{"snake", "snake_case", perLine(joinWords("_", strings.ToLower))},
		{"screaming", "SCREAMING_SNAKE", perLine(joinWords("_", strings.ToUpper))},
		{"kebab", "kebab-case", perLine(joinWords("-", strings.ToLower))},
		{"dot", "dot.case", perLine(joinWords(".", strings.ToLower))},
		{},
		{"upper", i18n.Mark("UPPERCASE"), always(strings.ToUpper)},
		{"lower", i18n.Mark("lowercase"), always(strings.ToLower)},
		{"title", i18n.Mark("Title Case"), always(titleCase)},
		{"sentence", i18n.Mark("Sentence case"), always(sentenceCase)},
		{"swap", i18n.Mark("iNVERT cASE"), always(swapCase)},
	}, ""},
	{i18n.Mark("Spaces and lines"), []Op{
		{"squeeze", i18n.Mark("Collapse extra spaces"), always(squeezeSpaces)},
		{"join", i18n.Mark("Join into one line"), always(joinLines)},
		{"sort", i18n.Mark("Sort lines"), lines(sortLines)},
		{"unique", i18n.Mark("Remove duplicate lines"), lines(uniqueLines)},
		{"reverse", i18n.Mark("Reverse line order"), lines(reverseLines)},
	}, ""},
	{i18n.Mark("Encoding"), []Op{
		{"url-encode", i18n.Mark("URL: encode"), always(url.QueryEscape)},
		{"url-decode", i18n.Mark("URL: decode"), urlDecode},
		{"base64-encode", i18n.Mark("Base64: encode"), always(func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) })},
		{"base64-decode", i18n.Mark("Base64: decode"), base64Decode},
	}, "encoding"},
	{i18n.Mark("Quotes"), []Op{
		{"guillemets", i18n.Mark("«Guillemets»"), always(wrap("«", "»"))},
		{"curly-quotes", i18n.Mark("“Curly quotes”"), always(wrap("“", "”"))},
		{"straight-quotes", i18n.Mark("\"Straight quotes\""), always(wrap(`"`, `"`))},
		{"backticks", i18n.Mark("`Backticks`"), always(wrap("`", "`"))},
		{},
		{"unquote", i18n.Mark("Remove quotes"), unquote},
	}, "quotes"},
}

func wrap(open, close string) func(string) string {
	return func(s string) string { return open + s + close }
}

// Quote marks that can open and close a text: Ukrainian, English and German styles, straight ones and backticks.
const (
	openQuotes  = "«“„\"'`‘"
	closeQuotes = "»”“\"'`’"
)

// unquote takes off the marks around the whole text, keeping the spaces outside them.
func unquote(s string) (string, bool) {
	t := strings.TrimSpace(s)
	first, n1 := utf8.DecodeRuneInString(t)
	last, n2 := utf8.DecodeLastRuneInString(t)
	if len(t) < n1+n2 || !strings.ContainsRune(openQuotes, first) || !strings.ContainsRune(closeQuotes, last) {
		return s, false
	}
	start := strings.Index(s, t)
	return s[:start] + t[n1:len(t)-n2] + s[start+len(t):], true
}

// Find looks up an operation by ID.
func Find(id string) (Op, bool) {
	for _, g := range Groups {
		for _, o := range g.Ops {
			if o.ID == id && o.Apply != nil {
				return o, true
			}
		}
	}
	return Op{}, false
}

func always(f func(string) string) func(string) (string, bool) {
	return func(s string) (string, bool) { return f(s), true }
}

// perLine applies f to each line separately: in a selection of several identifiers, each stays on its own line.
func perLine(f func(string) string) func(string) (string, bool) {
	return func(s string) (string, bool) {
		ls := strings.Split(s, "\n")
		for i, l := range ls {
			body, cr := strings.CutSuffix(l, "\r")
			ls[i] = f(body)
			if cr {
				ls[i] += "\r"
			}
		}
		return strings.Join(ls, "\n"), true
	}
}

// lines is for operations on a list of lines; sorting a single line makes no sense.
func lines(f func([]string) []string) func(string) (string, bool) {
	return func(s string) (string, bool) {
		nl := "\n"
		if strings.Contains(s, "\r\n") {
			nl = "\r\n"
		}
		body, trailing := strings.CutSuffix(s, nl)
		ls := strings.Split(body, nl)
		if len(ls) < 2 {
			return s, false
		}
		out := strings.Join(f(ls), nl)
		if trailing {
			out += nl
		}
		return out, true
	}
}

// Words splits an identifier or phrase into words: by separators and case boundaries
// ("parseHTTPResponse2" → parse, HTTP, Response2). Digits stay with the preceding word.
func Words(s string) []string {
	var words []string
	rs := []rune(s)
	start := -1
	flush := func(end int) {
		if start >= 0 {
			words = append(words, string(rs[start:end]))
			start = -1
		}
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush(i)
			continue
		}
		if start >= 0 && unicode.IsUpper(r) {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			// fooBar → foo|Bar; HTTPServer → HTTP|Server; v2Beta → v2|Beta
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush(i)
			}
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(rs))
	return words
}

// keepEdges preserves the indentation and trailing whitespace of a line; only the identifier itself changes.
func keepEdges(s string, f func(string) string) string {
	trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
	lead := s[:len(s)-len(trimmed)]
	core := strings.TrimRightFunc(trimmed, unicode.IsSpace)
	if core == "" {
		return s
	}
	return lead + f(core) + trimmed[len(core):]
}

func joinWords(sep string, f func(string) string) func(string) string {
	return func(s string) string {
		return keepEdges(s, func(core string) string { return f(strings.Join(Words(core), sep)) })
	}
}

func camel(s string) string {
	return keepEdges(s, func(core string) string {
		ws := Words(core)
		for i, w := range ws {
			if i == 0 {
				ws[i] = strings.ToLower(w)
			} else {
				ws[i] = capitalize(w)
			}
		}
		return strings.Join(ws, "")
	})
}

func pascal(s string) string {
	return keepEdges(s, func(core string) string {
		ws := Words(core)
		for i, w := range ws {
			ws[i] = capitalize(w)
		}
		return strings.Join(ws, "")
	})
}

func capitalize(w string) string {
	r, n := utf8.DecodeRuneInString(w)
	return string(unicode.ToUpper(r)) + strings.ToLower(w[n:])
}

// titleCase capitalizes the first letter of each word and lowercases the rest; an apostrophe does not break a word (п'ять → П'ять).
func titleCase(s string) string {
	var b strings.Builder
	prev := ' '
	for _, r := range s {
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || (isApostrophe(prev) && unicode.IsLetter(r)) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(unicode.ToUpper(r))
		}
		prev = r
	}
	return b.String()
}

// sentenceCase capitalizes the start of the text, after .!? and at the start of each line.
func sentenceCase(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			if start {
				r = unicode.ToUpper(r)
			} else {
				r = unicode.ToLower(r)
			}
			start = false
		case r == '.' || r == '!' || r == '?' || r == '\n':
			start = true
		}
		b.WriteRune(r)
	}
	return b.String()
}

func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// squeezeSpaces collapses runs of spaces and tabs to one space, trims them at line edges
// and collapses consecutive empty lines.
func squeezeSpaces(s string) string {
	nl := "\n"
	if strings.Contains(s, "\r\n") {
		nl = "\r\n"
	}
	var out []string
	for _, l := range strings.Split(s, nl) {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, nl)
}

func joinLines(s string) string { return strings.Join(strings.Fields(s), " ") }

func sortLines(ls []string) []string {
	slices.SortStableFunc(ls, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return ls
}

func uniqueLines(ls []string) []string {
	seen := map[string]bool{}
	out := ls[:0]
	for _, l := range ls {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func reverseLines(ls []string) []string {
	slices.Reverse(ls)
	return ls
}

func urlDecode(s string) (string, bool) {
	d, err := url.QueryUnescape(strings.TrimSpace(s))
	return d, err == nil
}

// base64Decode accepts both the standard and URL-safe alphabets, with or without padding; the result must be text.
func base64Decode(s string) (string, bool) {
	s = strings.Join(strings.Fields(s), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if d, err := enc.DecodeString(s); err == nil {
			return string(d), utf8.Valid(d) && len(d) > 0
		}
	}
	return s, false
}

func isApostrophe(r rune) bool { return r == '\'' || r == '’' || r == 'ʼ' || r == '`' }
