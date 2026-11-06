package i18n

import "testing"

const samplePO = `# Коментар перекладача
msgid ""
msgstr ""
"Language: uk\n"
"Plural-Forms: nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);\n"

#: tray.go
msgid "Enabled"
msgstr "Увімкнено"

#, fuzzy
msgid "Exit"
msgstr "Вихід?"

msgid "Untranslated"
msgstr ""

msgid "Long "
"line"
msgstr "Довгий "
"рядок"
msgid "%d character"
msgid_plural "%d characters"
msgstr[0] "%d символ"
msgstr[1] "%d символи"
msgstr[2] "%d символів"
`

func TestParsePO(t *testing.T) {
	c, err := parsePO(samplePO)
	if err != nil {
		t.Fatal(err)
	}
	cur.Store(c)
	defer cur.Store(nil)

	for in, want := range map[string]string{
		"Enabled":      "Увімкнено",
		"Exit":         "Exit", // fuzzy is not applied
		"Untranslated": "Untranslated",
		"Long line":    "Довгий рядок",
		"Missing":      "Missing",
	} {
		if got := T(in); got != want {
			t.Errorf("T(%q) = %q, want %q", in, got, want)
		}
	}
	for n, want := range map[int]string{
		1: "1 символ", 2: "2 символи", 5: "5 символів", 11: "11 символів", 21: "21 символ", 22: "22 символи", 112: "112 символів",
	} {
		if got := Nf("%d character", "%d characters", n); got != want {
			t.Errorf("Nf(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPluralForms(t *testing.T) {
	for _, tt := range []struct {
		expr string
		n    []int
		want []int
	}{
		{"nplurals=2; plural=(n != 1);", []int{0, 1, 2}, []int{1, 0, 1}},
		{"nplurals=2; plural=n>1;", []int{0, 1, 2}, []int{0, 0, 1}},
		{"nplurals=1; plural=0;", []int{1, 5}, []int{0, 0}},
		{"nplurals=3; plural=(n==1) ? 0 : (n>=2 && n<=4) ? 1 : 2;", []int{1, 3, 5}, []int{0, 1, 2}},
		{"nplurals=2; plural=!(n==1);", []int{1, 2}, []int{0, 1}},
	} {
		f, err := parsePluralForms(tt.expr)
		if err != nil {
			t.Errorf("%s: %v", tt.expr, err)
			continue
		}
		for i, n := range tt.n {
			if got := f(n); got != tt.want[i] {
				t.Errorf("%s: f(%d) = %d, want %d", tt.expr, n, got, tt.want[i])
			}
		}
	}
	if _, err := parsePluralForms("plural=(n+;"); err == nil {
		t.Error("a broken expression should have produced an error")
	}
}
