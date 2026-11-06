// Package i18n translates the interface through plain gettext files (.po).
//
// Strings in the code are English (msgid); translations live in resources/locales/<lang>.po, embedded in the program.
// A file %APPDATA%\selkit\locales\<lang>.po takes precedence over the embedded one, so a translation can be
// added or fixed without rebuilding. The template for translators is resources/locales/selkit.pot
// (updated by a test: SELKIT_UPDATE_POT=1 go test ./internal/i18n).
package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/Spell6inder/selkit/resources"
)

var embedded = resources.Locales

var cur atomic.Pointer[catalog]

var curLang atomic.Value // string

// Init picks the language: lang from the config or, if empty, the system UI language.
// userDir is the folder with the user's own .po files. It returns the chosen language ("en" means no translation).
func Init(lang, userDir string) (string, error) {
	cands := []string{lang}
	if lang == "" {
		cands = systemLanguages()
	}
	for _, l := range cands {
		base := strings.SplitN(l, "-", 2)[0]
		if strings.EqualFold(base, "en") {
			break // English is the language of the code itself; do not check further system languages
		}
		for _, code := range []string{l, base} { // uk-UA, then uk
			c, err := load(code, userDir)
			if err != nil {
				return "en", err
			}
			if c != nil {
				cur.Store(c)
				curLang.Store(code)
				return code, nil
			}
		}
	}
	cur.Store(nil)
	curLang.Store("en")
	return "en", nil
}

// Lang is the language chosen by Init ("uk", "uk-UA"); "en" before Init or without a translation.
func Lang() string {
	if l, ok := curLang.Load().(string); ok {
		return l
	}
	return "en"
}

func load(code, userDir string) (*catalog, error) {
	code = strings.ToLower(code)
	if code == "" || code == "en" {
		return nil, nil
	}
	if userDir != "" {
		if data, err := os.ReadFile(filepath.Join(userDir, code+".po")); err == nil {
			c, err := parsePO(string(data))
			if err != nil {
				return nil, fmt.Errorf("%s.po: %w", code, err)
			}
			return c, nil
		}
	}
	data, err := embedded.ReadFile("locales/" + code + ".po")
	if err != nil {
		return nil, nil
	}
	return parsePO(string(data))
}

// Languages returns the codes of languages that have a translation (embedded or user-provided), plus "en".
func Languages(userDir string) []string {
	langs := []string{"en"}
	add := func(name string) {
		if code, ok := strings.CutSuffix(strings.ToLower(name), ".po"); ok && !slices.Contains(langs, code) {
			langs = append(langs, code)
		}
	}
	entries, _ := embedded.ReadDir("locales")
	for _, e := range entries {
		add(e.Name())
	}
	if userDir != "" {
		entries, _ := os.ReadDir(userDir)
		for _, e := range entries {
			add(e.Name())
		}
	}
	return langs
}

// T translates a string.
func T(msgid string) string {
	if c := cur.Load(); c != nil {
		if m, ok := c.msgs[msgid]; ok && m[0] != "" {
			return m[0]
		}
	}
	return msgid
}

// Tf is T for a string with fmt verbs.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// N picks the plural form for n (one/other in English; in a translation, as many forms as the language has).
// Both strings usually contain %d, which Nf fills in.
func N(singular, plural string, n int) string {
	if c := cur.Load(); c != nil {
		if m, ok := c.msgs[singular]; ok && len(m) > 1 {
			if i := c.plural(n); i < len(m) && m[i] != "" {
				return m[i]
			}
		}
	}
	if n == 1 {
		return singular
	}
	return plural
}

// Nf is N with substitution: Nf("%d line", "%d lines", n) → "3 рядки".
func Nf(singular, plural string, n int, args ...any) string {
	return fmt.Sprintf(N(singular, plural, n), append([]any{n}, args...)...)
}

// Mark only marks a string for translation where translation happens later (names in tables): T(x) at display time.
func Mark(msgid string) string { return msgid }

// Catalog is a separate translation, such as a plugin's own: its folder holds <lang>.po files.
type Catalog struct{ c *catalog }

// LoadCatalog reads the translation for the current language from dir; without one, strings stay as they are.
func LoadCatalog(dir string) (*Catalog, error) {
	lang := strings.ToLower(Lang())
	for _, code := range []string{lang, strings.SplitN(lang, "-", 2)[0]} {
		if code == "en" {
			break
		}
		data, err := os.ReadFile(filepath.Join(dir, code+".po"))
		if err != nil {
			continue
		}
		c, err := parsePO(string(data))
		if err != nil {
			return &Catalog{}, fmt.Errorf("%s.po: %w", code, err)
		}
		return &Catalog{c}, nil
	}
	return &Catalog{}, nil
}

// T translates a string of this catalog.
func (k *Catalog) T(msgid string) string {
	if k != nil && k.c != nil {
		if m, ok := k.c.msgs[msgid]; ok && m[0] != "" {
			return m[0]
		}
	}
	return msgid
}
