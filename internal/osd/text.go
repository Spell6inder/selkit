package osd

import (
	"strings"

	"github.com/Spell6inder/selkit/internal/i18n"
)

const (
	previewLines = 4
	previewRunes = 120
)

// Preview prepares text for the tooltip: first lines without blank edges, long ones truncated with "…".
func Preview(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "    ") // DrawText expands a tab to 8 characters, which is too wide for code
	s = strings.Trim(s, "\n")

	lines := strings.Split(s, "\n")
	more := len(lines) > previewLines
	if more {
		lines = lines[:previewLines]
	}
	for i, l := range lines {
		if r := []rune(l); len(r) > previewRunes {
			lines[i] = string(r[:previewRunes]) + "…"
		}
	}
	if more {
		lines[len(lines)-1] += " …"
	}
	return strings.Join(lines, "\n")
}

// Title is the tooltip title: what happened and how much text.
func Title(action, s string) string {
	runes := len([]rune(s))
	lines := strings.Count(strings.ReplaceAll(s, "\r\n", "\n"), "\n") + 1
	t := action + " · " + i18n.Nf("%d character", "%d characters", runes)
	if lines > 1 {
		t += ", " + i18n.Nf("%d line", "%d lines", lines)
	}
	return t
}
