// Command mark writes resources/icon/selkit.svg and selkit-16.svg: the letters as outlines of Liberation Sans
// Bold (SIL Open Font License, metrics of Arial), so that the icon needs no font, and the mark centred by the
// outlines themselves rather than by the advances of the letters.
//
//	go run ./internal/icon/mark LiberationSans-Bold.ttf resources/icon
//
// The font is not in the repository: Liberation Sans is in most Linux distributions (fonts-liberation) and at
// github.com/liberationfonts. Only its outlines end up in the icon, which the license allows.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	blue   = "#2563EB"
	amber  = "#FBBF24"
	navy   = "#1E3A8A"
	white  = "#FFFFFF"
	gold   = "#FBBF24"
	dotlss = 'ı' // the dot is the star
)

type box struct{ x0, y0, x1, y1 float64 }

func (b box) union(o box) box {
	return box{math.Min(b.x0, o.x0), math.Min(b.y0, o.y0), math.Max(b.x1, o.x1), math.Max(b.y1, o.y1)}
}

type glyph struct {
	d   string // path data at its place
	ink box
	adv float64
}

var f *sfnt.Font

// outline is a letter at size px with its origin at (x, baseline).
func outline(r rune, px, x, baseline float64) glyph {
	var b sfnt.Buffer
	gi, err := f.GlyphIndex(&b, r)
	check(err)
	ppem := fixed.Int26_6(px * 64)
	segs, err := f.LoadGlyph(&b, gi, ppem, nil)
	check(err)
	adv, err := f.GlyphAdvance(&b, gi, ppem, font.HintingNone)
	check(err)
	g := glyph{adv: float64(adv) / 64, ink: box{1e9, 1e9, -1e9, -1e9}}
	var sb strings.Builder
	pt := func(p fixed.Point26_6) (float64, float64) {
		px, py := x+float64(p.X)/64, baseline+float64(p.Y)/64
		return px, py
	}
	add := func(ps ...fixed.Point26_6) string {
		var s []string
		for _, p := range ps {
			px, py := pt(p)
			g.ink = g.ink.union(box{px, py, px, py})
			s = append(s, fmt.Sprintf("%s %s", num(px), num(py)))
		}
		return strings.Join(s, " ")
	}
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if sb.Len() > 0 {
				sb.WriteString("Z")
			}
			sb.WriteString("M" + add(s.Args[0]))
		case sfnt.SegmentOpLineTo:
			sb.WriteString("L" + add(s.Args[0]))
		case sfnt.SegmentOpQuadTo:
			sb.WriteString("Q" + add(s.Args[0], s.Args[1]))
		case sfnt.SegmentOpCubeTo:
			sb.WriteString("C" + add(s.Args[0], s.Args[1], s.Args[2]))
		}
	}
	sb.WriteString("Z")
	g.d = sb.String()
	return g
}

func num(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	return s
}

// star is a regular five-pointed star of outer radius r, pointing up.
func star(cx, cy, r float64) (string, box) {
	var pts []string
	b := box{1e9, 1e9, -1e9, -1e9}
	for i := 0; i < 10; i++ {
		rr := r
		if i%2 == 1 {
			rr = r * 0.382
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		x, y := cx+rr*math.Cos(a), cy+rr*math.Sin(a)
		b = b.union(box{x, y, x, y})
		pts = append(pts, num(x)+" "+num(y))
	}
	return "M" + strings.Join(pts, " L") + "Z", b
}

// roundRect is a rounded rectangle as a path, which every SVG reader draws the same.
func roundRect(x, y, w, h, r float64) string {
	return fmt.Sprintf("M%s %sH%sA%s %s 0 0 1 %s %sV%sA%s %s 0 0 1 %s %sH%sA%s %s 0 0 1 %s %sV%sA%s %s 0 0 1 %s %sZ",
		num(x+r), num(y), num(x+w-r), num(r), num(r), num(x+w), num(y+r), num(y+h-r), num(r), num(r), num(x+w-r), num(y+h),
		num(x+r), num(r), num(r), num(x), num(y+h-r), num(y+r), num(r), num(r), num(x+r), num(y))
}

type shape struct{ fill, d string }

// word lays out the letters from x on the baseline, with extra space after the letter at gapAfter.
func word(s string, px, x, baseline float64, gapAfter int, gap float64) []glyph {
	var out []glyph
	for i, r := range []rune(s) {
		g := outline(r, px, x, baseline)
		out = append(out, g)
		x += g.adv
		if i == gapAfter {
			x += gap
		}
	}
	return out
}

// write centres the shapes in a size×size icon on the background, by their bounds, and writes the file.
func write(path, comment string, size float64, bg string, shapes []shape, bounds box) {
	dx := (size-(bounds.x1-bounds.x0))/2 - bounds.x0
	dy := (size-(bounds.y1-bounds.y0))/2 - bounds.y0
	var sb strings.Builder
	fmt.Fprintf(&sb, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 %s %s\" width=\"%s\" height=\"%s\">\n", num(size), num(size), num(size*4), num(size*4))
	fmt.Fprintf(&sb, "  <!-- %s\n       Made by internal/icon/mark from Liberation Sans Bold (SIL Open Font License); centred on the outlines:\n       %s left and right, %s top and bottom. -->\n",
		comment, num(bounds.x0+dx), num(bounds.y0+dy))
	fmt.Fprintf(&sb, "  <path fill=\"%s\" d=\"%s\"/>\n", blue, bg)
	fmt.Fprintf(&sb, "  <g transform=\"translate(%s %s)\">\n", num(dx), num(dy))
	for _, s := range shapes {
		fmt.Fprintf(&sb, "    <path fill=\"%s\" d=\"%s\"/>\n", s.fill, s.d)
	}
	sb.WriteString("  </g>\n</svg>\n")
	check(os.WriteFile(path, []byte(sb.String()), 0o644))
	fmt.Printf("%s: shift %s %s, margins %s/%s\n", filepath.Base(path), num(dx), num(dy), num(bounds.x0+dx), num(bounds.y0+dy))
}

func main() {
	data, err := os.ReadFile(os.Args[1])
	check(err)
	f, err = sfnt.Parse(data)
	check(err)
	dir := os.Args[2]

	// The full mark: "Sel" on the highlight, then "kıt" a little apart, the star as the dot of the i.
	letters := word("Selk"+string(dotlss)+"t", 16, 7.5, 39.5, 2, 2)
	sel, rest := letters[:3], letters[3:]
	inkSel := sel[0].ink.union(sel[2].ink)
	hl := box{inkSel.x0 - 2.4, 25, math.Min(inkSel.x1+2.4, rest[0].ink.x0-0.8), 44}
	i := rest[1]
	sd, sb := star((i.ink.x0+i.ink.x1)/2, i.ink.y0-4.6, 4.3)
	shapes := []shape{{amber, roundRect(hl.x0, hl.y0, hl.x1-hl.x0, hl.y1-hl.y0, 3)}}
	all := hl.union(sb)
	for _, g := range sel {
		shapes = append(shapes, shape{navy, g.d})
	}
	for _, g := range rest {
		shapes = append(shapes, shape{white, g.d})
		all = all.union(g.ink)
	}
	shapes = append(shapes, shape{gold, sd})
	write(filepath.Join(dir, "selkit.svg"), "Selkit, 32 px and up: \"Sel\" highlighted, and the dot of the i is a gold star.",
		64, roundRect(2, 2, 60, 60, 14), shapes, all)

	// 16–24 px: the letters would not read, so an S on the highlight and the star.
	s := outline('S', 9.5, 0, 0)
	w, h := s.ink.x1-s.ink.x0, s.ink.y1-s.ink.y0
	hl16 := box{2, 4.5, 10.5, 13}
	s = outline('S', 9.5, (hl16.x0+hl16.x1)/2-w/2-s.ink.x0, (hl16.y0+hl16.y1)/2-h/2-s.ink.y0)
	sd16, sb16 := star(12.4, 4.3, 3)
	write(filepath.Join(dir, "selkit-16.svg"), "Simplified Selkit mark for 16–24 px: an S on the highlight and the star.",
		16, roundRect(0.5, 0.5, 15, 15, 3.5),
		[]shape{{amber, roundRect(hl16.x0, hl16.y0, hl16.x1-hl16.x0, hl16.y1-hl16.y0, 1.5)}, {navy, s.d}, {gold, sd16}},
		hl16.union(sb16))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
