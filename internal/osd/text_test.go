package osd

import (
	"strings"
	"testing"
)

func TestPreview(t *testing.T) {
	long := strings.Repeat("я", previewRunes+5)
	tests := []struct{ in, want string }{
		{"просто текст", "просто текст"},
		{"\r\n\r\nрядок\r\nдругий\r\n\r\n", "рядок\nдругий"},
		{"1\n2\n3\n4\n5\n6", "1\n2\n3\n4 …"},
		{"if x {\n\treturn\n}", "if x {\n    return\n}"},
		{long, strings.Repeat("я", previewRunes) + "…"},
	}
	for _, tt := range tests {
		if got := Preview(tt.in); got != tt.want {
			t.Errorf("Preview(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTitle(t *testing.T) {
	if got := Title("Primary", "абв"); got != "Primary · 3 characters" {
		t.Errorf("single-line: %q", got)
	}
	if got := Title("Pasted", "a\r\nb"); got != "Pasted · 4 characters, 2 lines" {
		t.Errorf("multi-line: %q", got)
	}
}
