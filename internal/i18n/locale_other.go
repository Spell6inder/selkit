//go:build !windows

package i18n

import (
	"os"
	"strings"
)

// systemLanguages lists the preferred languages from the POSIX locale variables, as BCP 47 tags ("uk-UA").
func systemLanguages() []string {
	var langs []string
	if l := os.Getenv("LANGUAGE"); l != "" { // GNU extension: a list such as "uk:en"
		langs = strings.Split(l, ":")
	}
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := os.Getenv(v); l != "" {
			langs = append(langs, l)
			break
		}
	}
	out := langs[:0]
	for _, l := range langs {
		l, _, _ = strings.Cut(l, ".") // uk_UA.UTF-8 → uk_UA
		l, _, _ = strings.Cut(l, "@")
		if l != "" && l != "C" && l != "POSIX" {
			out = append(out, strings.ReplaceAll(l, "_", "-"))
		}
	}
	return out
}

// Native names of the languages we ship; others are shown by their code.
var nativeNames = map[string]string{"en": "English", "uk": "українська"}

// LanguageName is the name of a language in that language itself ("українська", "English").
func LanguageName(code string) string {
	base, _, _ := strings.Cut(code, "-")
	if n, ok := nativeNames[base]; ok {
		return n
	}
	return code
}
