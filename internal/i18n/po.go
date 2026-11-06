package i18n

import (
	"fmt"
	"strconv"
	"strings"
)

type catalog struct {
	msgs   map[string][]string // msgid → translation (for plurals, one form per index)
	plural func(n int) int
}

type entry struct {
	ctx, id, idPlural string
	strs              []string
	fuzzy             bool
	ref               []string // "#:" comments: where the string comes from (template only)
}

// parsePO reads a .po file: msgid / msgid_plural / msgstr / msgstr[N], multi-line strings, escapes.
// Like gettext, it skips untranslated and "fuzzy" entries.
func parsePO(src string) (*catalog, error) {
	entries, err := parseEntries(src)
	if err != nil {
		return nil, err
	}
	c := &catalog{msgs: map[string][]string{}, plural: func(n int) int {
		if n == 1 {
			return 0
		}
		return 1
	}}
	for _, e := range entries {
		if e.id == "" { // header
			for _, line := range strings.Split(e.strs[0], "\n") {
				if v, ok := strings.CutPrefix(line, "Plural-Forms:"); ok {
					f, err := parsePluralForms(v)
					if err != nil {
						return nil, fmt.Errorf("Plural-Forms: %w", err)
					}
					c.plural = f
				}
			}
			continue
		}
		if e.fuzzy || len(e.strs) == 0 || e.strs[0] == "" {
			continue
		}
		c.msgs[e.id] = e.strs
	}
	return c, nil
}

func parseEntries(src string) ([]entry, error) {
	var (
		out  []entry
		e    entry
		dst  *string // where to append a continuation string
		used bool
	)
	flush := func() {
		if used {
			out = append(out, e)
		}
		e, dst, used = entry{}, nil, false
	}
	for n, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flush()
			continue
		case strings.HasPrefix(line, "#"):
			if used && len(e.strs) > 0 { // a comment after msgstr already belongs to the next entry
				flush()
			}
			dst = nil
			switch {
			case strings.HasPrefix(line, "#,"):
				e.fuzzy = e.fuzzy || strings.Contains(line, "fuzzy")
			case strings.HasPrefix(line, "#:"):
				e.ref = append(e.ref, strings.TrimSpace(line[2:]))
			}
			continue
		case strings.HasPrefix(line, `"`):
			if dst == nil {
				return nil, fmt.Errorf("line %d: continuation without a keyword", n+1)
			}
			s, err := strconv.Unquote(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n+1, err)
			}
			*dst += s
			continue
		}
		key, rest, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("line %d: cannot parse %q", n+1, line)
		}
		s, err := strconv.Unquote(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		if (key == "msgid" || key == "msgctxt") && used && len(e.strs) > 0 {
			flush() // a new entry without a blank line between them
		}
		used = true
		switch {
		case key == "msgctxt":
			e.ctx, dst = s, &e.ctx
		case key == "msgid":
			e.id, dst = s, &e.id
		case key == "msgid_plural":
			e.idPlural, dst = s, &e.idPlural
		case key == "msgstr":
			e.strs = []string{s}
			dst = &e.strs[0]
		case strings.HasPrefix(key, "msgstr["):
			i, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(key, "msgstr["), "]"))
			if err != nil || i != len(e.strs) {
				return nil, fmt.Errorf("line %d: %s out of order", n+1, key)
			}
			e.strs = append(e.strs, s)
			dst = &e.strs[i]
		default:
			return nil, fmt.Errorf("line %d: unknown keyword %q", n+1, key)
		}
	}
	flush()
	return out, nil
}

// parsePluralForms parses "nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : …);", a C expression in n.
func parsePluralForms(s string) (func(int) int, error) {
	_, expr, ok := strings.Cut(s, "plural=")
	if !ok {
		return nil, fmt.Errorf("no plural=")
	}
	expr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(expr), ";"))
	p := &exprParser{src: expr}
	f := p.ternary()
	if p.err == nil && p.skipSpace() < len(p.src) {
		p.err = fmt.Errorf("trailing garbage: %q", p.src[p.pos:])
	}
	if p.err != nil {
		return nil, p.err
	}
	return func(n int) int { return f(n) }, nil
}

// exprParser is a recursive-descent parser for the subset of C used by Plural-Forms.
type exprParser struct {
	src string
	pos int
	err error
}

type fn = func(n int) int

func (p *exprParser) skipSpace() int {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
	return p.pos
}

func (p *exprParser) eat(op string) bool {
	p.skipSpace()
	if strings.HasPrefix(p.src[p.pos:], op) {
		// "<" must not swallow the start of "<=", nor "!" the start of "!=".
		if next := p.pos + len(op); (op == "<" || op == ">" || op == "!") && next < len(p.src) && p.src[next] == '=' {
			return false
		}
		p.pos += len(op)
		return true
	}
	return false
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (p *exprParser) ternary() fn {
	c := p.binary(0)
	if !p.eat("?") {
		return c
	}
	a := p.ternary()
	if !p.eat(":") {
		p.err = fmt.Errorf("expected ':' at position %d", p.pos)
		return c
	}
	b := p.ternary()
	return func(n int) int {
		if c(n) != 0 {
			return a(n)
		}
		return b(n)
	}
}

// Precedence levels, lowest first.
var levels = [][]string{{"||"}, {"&&"}, {"==", "!="}, {"<=", ">=", "<", ">"}, {"+", "-"}, {"*", "/", "%"}}

func (p *exprParser) binary(level int) fn {
	if level == len(levels) {
		return p.unary()
	}
	l := p.binary(level + 1)
	for {
		op := ""
		for _, o := range levels[level] {
			if p.eat(o) {
				op = o
				break
			}
		}
		if op == "" {
			return l
		}
		r, left := p.binary(level+1), l
		switch op {
		case "||":
			l = func(n int) int { return b2i(left(n) != 0 || r(n) != 0) }
		case "&&":
			l = func(n int) int { return b2i(left(n) != 0 && r(n) != 0) }
		case "==":
			l = func(n int) int { return b2i(left(n) == r(n)) }
		case "!=":
			l = func(n int) int { return b2i(left(n) != r(n)) }
		case "<=":
			l = func(n int) int { return b2i(left(n) <= r(n)) }
		case ">=":
			l = func(n int) int { return b2i(left(n) >= r(n)) }
		case "<":
			l = func(n int) int { return b2i(left(n) < r(n)) }
		case ">":
			l = func(n int) int { return b2i(left(n) > r(n)) }
		case "+":
			l = func(n int) int { return left(n) + r(n) }
		case "-":
			l = func(n int) int { return left(n) - r(n) }
		case "*":
			l = func(n int) int { return left(n) * r(n) }
		case "/", "%":
			div := op == "/"
			l = func(n int) int {
				d := r(n)
				if d == 0 {
					return 0
				}
				if div {
					return left(n) / d
				}
				return left(n) % d
			}
		}
	}
}

func (p *exprParser) unary() fn {
	if p.eat("!") {
		f := p.unary()
		return func(n int) int { return b2i(f(n) == 0) }
	}
	if p.eat("(") {
		f := p.ternary()
		if !p.eat(")") {
			p.err = fmt.Errorf("expected ')' at position %d", p.pos)
		}
		return f
	}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == 'n' {
		p.pos++
		return func(n int) int { return n }
	}
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	v, err := strconv.Atoi(p.src[start:p.pos])
	if err != nil {
		if p.err == nil {
			p.err = fmt.Errorf("expected a number or n at position %d", start)
		}
		return func(int) int { return 0 }
	}
	return func(int) int { return v }
}
