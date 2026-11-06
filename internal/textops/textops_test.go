package textops

import (
	"slices"
	"testing"
)

func op(t *testing.T, id string) Op {
	for _, g := range Groups {
		for _, o := range g.Ops {
			if o.ID == id {
				return o
			}
		}
	}
	t.Fatalf("no operation %q", id)
	return Op{}
}

func TestWords(t *testing.T) {
	for in, want := range map[string][]string{
		"parseHTTPResponse2": {"parse", "HTTP", "Response2"},
		"some_var-name.x":    {"some", "var", "name", "x"},
		"  Hello  world ":    {"Hello", "world"},
		"XMLHttpRequest":     {"XML", "Http", "Request"},
		"ім'яКористувача":    {"ім", "я", "Користувача"},
		"utf8Decoder":        {"utf8", "Decoder"},
	} {
		if got := Words(in); !slices.Equal(got, want) {
			t.Errorf("Words(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOps(t *testing.T) {
	tests := []struct{ op, in, want string }{
		{"words", "parseHTTPResponse", "parse HTTP Response"},
		{"words", "MAX_RETRY_COUNT\nsome-var.name", "MAX RETRY COUNT\nsome var name"},
		{"camel", "Hello big World", "helloBigWorld"},
		{"camel", "parse_http_response", "parseHttpResponse"},
		{"pascal", "parse-http-response", "ParseHttpResponse"},
		{"snake", "parseHTTPResponse", "parse_http_response"},
		{"screaming", "maxRetryCount", "MAX_RETRY_COUNT"},
		{"kebab", "Привіт Світ", "привіт-світ"},
		{"dot", "SomeKey", "some.key"},
		{"snake", "  fooBar\r\nbazQux\n", "  foo_bar\r\nbaz_qux\n"},
		{"title", "п'ять ДНІВ тому", "П'ять Днів Тому"},
		{"sentence", "ПЕРШЕ. ДРУГЕ? так", "Перше. Друге? Так"},
		{"swap", "пРИВІТ", "Привіт"},
		{"squeeze", "  a \t b  \n\n\n  c ", "a b\n\nc"},
		{"join", "a\r\n  b\nc", "a b c"},
		{"sort", "b\nA\na\n", "A\na\nb\n"},
		{"unique", "x\r\ny\r\nx", "x\r\ny"},
		{"reverse", "1\n2\n3", "3\n2\n1"},
		{"url-encode", "a b&c=д", "a+b%26c%3D%D0%B4"},
		{"url-decode", "a+b%26c", "a b&c"},
		{"base64-encode", "привіт", "0L/RgNC40LLRltGC"},
		{"base64-decode", "0L/RgNC40LLRltGC", "привіт"},
		{"base64-decode", "aGk", "hi"},
	}
	for _, tt := range tests {
		got, ok := op(t, tt.op).Apply(tt.in)
		if !ok || got != tt.want {
			t.Errorf("%s(%q) = %q, %v; want %q", tt.op, tt.in, got, ok, tt.want)
		}
	}
}

func TestOpsNotApplicable(t *testing.T) {
	for _, tt := range []struct{ op, in string }{
		{"base64-decode", "не base64"},
		{"base64-decode", "//79"}, // bytes, not text
		{"url-decode", "100%"},
		{"sort", "один рядок"},
	} {
		if got, ok := op(t, tt.op).Apply(tt.in); ok {
			t.Errorf("%s(%q) = %q - should have been inapplicable", tt.op, tt.in, got)
		}
	}
}

func TestQuotes(t *testing.T) {
	for _, c := range []struct{ id, in, out string }{
		{"guillemets", "Щука", "«Щука»"},
		{"curly-quotes", "fish", "“fish”"},
		{"straight-quotes", "a b", `"a b"`},
		{"backticks", "x", "`x`"},
		{"unquote", "«Щука»", "Щука"},
		{"unquote", "  “fish”\n", "  fish\n"},
		{"unquote", "„Hecht“", "Hecht"},
		{"unquote", "`code`", "code"},
		{"unquote", `""`, ""},
	} {
		op, ok := Find(c.id)
		if !ok {
			t.Fatalf("%s: not found", c.id)
		}
		if out, ok := op.Apply(c.in); !ok || out != c.out {
			t.Errorf("%s(%q) = %q, %v; want %q", c.id, c.in, out, ok, c.out)
		}
	}
	op, _ := Find("unquote")
	for _, in := range []string{"Щука", "«Щука", "Щука»", "«", "", "  "} {
		if out, ok := op.Apply(in); ok {
			t.Errorf("unquote(%q) = %q, but the text is not in quotes", in, out)
		}
	}
}
