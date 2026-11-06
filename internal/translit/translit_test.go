package translit

import (
	"os"
	"path/filepath"
	"testing"
)

func system(t *testing.T, id string) *System {
	t.Helper()
	set, err := Load(os.DirFS(filepath.Join("..", "..", "resources")), "translit", "")
	if err != nil {
		t.Fatal(err)
	}
	s := set.ByID(id)
	if s == nil {
		t.Fatalf("no system %q", id)
	}
	return s
}

func check(t *testing.T, name string, f func(string) (string, bool), cases map[string]string) {
	t.Helper()
	for in, want := range cases {
		if got, ok := f(in); !ok || got != want {
			t.Errorf("%s(%q) = %q, %v; want %q", name, in, got, ok, want)
		}
	}
}

func TestUkrainian(t *testing.T) {
	uk := system(t, "uk")
	check(t, "uk", uk.Forward, map[string]string{
		"Євген Юрій Їжакевич":        "Yevhen Yurii Yizhakevych",
		"Згорани, Розгон":            "Zghorany, Rozghon",
		"м'ята, Короп'є":             "miata, Koropie",
		"ЩУКА і Щука":                "SHCHUKA i Shchuka",
		"Ґанок, Жуків, Хмельницький": "Ganok, Zhukiv, Khmelnytskyi",
		"Hello, світ!":               "Hello, svit!",
		"Майя, Знам'янка":            "Maiia, Znamianka",
	})
	if _, ok := uk.Forward("plain latin"); ok {
		t.Error("Latin text should not have changed")
	}
	check(t, "uk⁻¹", uk.Reverse, map[string]string{
		"Yevhen Yurii":      "Євген Юрій",
		"Zghorany, Shchuka": "Згорани, Щука",
		"SHCHUKA i Mariia":  "ЩУКА і Марія",
		"pryvit, svit!":     "привіт, світ!",
		"Kharkiv Zhytomyr":  "Харків Житомир",
		"Khmelnytskyi 2026": "Хмелницкий 2026",
	})
	// Ordinary words survive a round trip.
	for _, s := range []string{"привіт світ", "Шевченко", "Запоріжжя", "Чернігів", "Щастя", "Юрій Яковенко"} {
		lat, _ := uk.Forward(s)
		if back, _ := uk.Reverse(lat); back != s {
			t.Errorf("%q → %q → %q", s, lat, back)
		}
	}
}

func TestISO9(t *testing.T) {
	iso := system(t, "iso9")
	check(t, "iso9", iso.Forward, map[string]string{
		"Щука, Жито":    "Ŝuka, Žito",
		"Ґанок, Їжак":   "G̀anok, Ïžak",
		"ЧЕРНІГІВ":      "ČERNÌGÌV",
		"Љубав, Њујорк": "L̂ubav, N̂uǰork",
	})
	// ISO 9 is one-to-one, so every word comes back exactly.
	for _, s := range []string{"Щука", "Ґанок", "Їжак", "Чернігів", "Љубав", "Ђорђе", "България"} {
		lat, _ := iso.Forward(s)
		if back, _ := iso.Reverse(lat); back != s {
			t.Errorf("%q → %q → %q", s, lat, back)
		}
	}
}

func TestSerbian(t *testing.T) {
	sr := system(t, "sr")
	check(t, "sr", sr.Forward, map[string]string{
		"Љубав, Ђорђе, Џеп": "Ljubav, Đorđe, Džep",
		"ЋИРИЛИЦА":          "ĆIRILICA",
	})
	check(t, "sr⁻¹", sr.Reverse, map[string]string{
		"Ljubav, Đorđe, Džep": "Љубав, Ђорђе, Џеп",
	})
}

func TestBulgarian(t *testing.T) {
	bg := system(t, "bg")
	check(t, "bg", bg.Forward, map[string]string{
		"София, щастие":     "Sofia, shtastie",
		"България, Пловдив": "Balgaria, Plovdiv",
	})
}

func TestJapaneseKana(t *testing.T) {
	ja := system(t, "ja")
	check(t, "ja", ja.Forward, map[string]string{
		"葬送のフリーレン":  "葬送 no Furīren",
		"きって":       "kitte",
		"とうきょう":     "tōkyō",
		"マッチ":       "Matchi",
		"すし と てんぷら": "sushi to tenpura",
	})
	if ja.Reversible() {
		t.Error("kana romanization has no reverse rules")
	}
}

func TestRank(t *testing.T) {
	set, err := Load(os.DirFS(filepath.Join("..", "..", "resources")), "translit", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ text, lang, want string }{
		{"Їжак", "en", "uk"},
		{"Ђорђе", "uk", "sr"},
		{"съм", "en", "bg"},
		{"добро", "uk", "uk"}, // no telling letters: the interface language decides
		{"フリーレン", "uk", "ja"},
	} {
		got := set.ToLatin(c.text, c.lang)
		if len(got) == 0 || got[0].ID != c.want {
			t.Errorf("ToLatin(%q, %s) starts with %v, want %s", c.text, c.lang, ids(got), c.want)
		}
	}
	if got := set.FromLatin("pryvit", "uk"); len(got) == 0 || got[0].ID != "uk" {
		t.Errorf("FromLatin(pryvit) = %v, want uk first", ids(got))
	}
}

func TestUserRules(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "uk.rules"), []byte("name = Mine\nbase = iso9\n[to-latin]\nщ = sch\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.rules"), []byte("no sections here\n"), 0o644)
	set, err := Load(os.DirFS(filepath.Join("..", "..", "resources")), "translit", dir)
	if err == nil {
		t.Error("a broken file should be reported")
	}
	uk := set.ByID("uk")
	if got, _ := uk.Forward("Щука"); uk.Name("") != "Mine" || got != "Schuka" {
		t.Errorf("user file did not replace the built-in one: %q, %q", uk.Name(""), got)
	}
}

func ids(ss []*System) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}
