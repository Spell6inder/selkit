# Transliteration rules

**English** · [Українська](uk/translit.md)

The **To Latin** / **From Latin** menu items use systems described in plain-text `.rules` files.

**Built-in systems**, in [`resources/translit`](../resources/translit):

| File | System | Back to the original script |
|---|---|---|
| `iso9.rules` | ISO 9:1995: every Cyrillic letter has exactly one Latin letter with diacritics. It is also the base for the national systems | exact |
| `uk.rules` | Ukrainian, official (Cabinet of Ministers resolution No. 55, 2010), as in passports | best guess |
| `bg.rules` | Bulgarian, official (Transliteration Act, 2009) | best guess |
| `sr.rules` | Serbian Latin alphabet | exact |
| `ja.rules` | Japanese kana, modified Hepburn. Kanji are left as they are | — |

**How a system is chosen.** The main item picks the system automatically:
1. by letters typical of a language (і ї є ґ → Ukrainian, ђ ћ џ љ њ ј → Serbian, ъ → Bulgarian);
2. then by the interface language;
3. then by priority.

The **Latin by** submenu lists every system that fits the text, and **Repeat** keeps the system you chose.

## Your own rules

Put `.rules` files into `%APPDATA%\selkit\translit` (on Linux, `~/.config/selkit/translit`) and restart Selkit.
- A file named like a built-in one (`uk.rules`) replaces it.
- A new name adds a system.
- Broken files are skipped and reported in the log.

A small example that changes one letter of the Ukrainian system and keeps everything else from ISO 9:

```ini
name    = Ukrainian (mine)
name.uk = Українська (моя)
lang    = uk
base    = iso9
detect  = і ї є ґ

[to-latin]
щ = sch
```

## Format

The file is UTF-8 text. It starts with `key = value` header settings, then has sections of `from = to` rules.

- **Comments.** `#` starts a comment at the start of a line or after a space.
- **Escapes.** Values may use `\uXXXX`, e.g. `ґ = g\u0300` for a combining accent.

**Header**

| Key | Meaning |
|---|---|
| `name`, `name.<lang>` | Display name, optionally per interface language (`name.uk`) |
| `lang` | Language of the system. It is preferred when this is the interface language |
| `base` | Another system whose rules fill in the letters not given here |
| `detect` | Letters that point to this system when choosing automatically |
| `detect-latin` | Letters without which Latin text is not taken back by this system (used by ISO 9) |
| `priority` | Tie-breaker for automatic choice; higher wins, the default is 0 |
| `reverse` | `rules` uses the `[from-latin]` section (the default if it exists). `auto` inverts `[to-latin]`, which suits one-to-one systems. `no` turns the reverse off |
| `silent` | Characters dropped between two converted letters, such as apostrophes |
| `fold-katakana` | `yes`: katakana is matched by the hiragana rules |
| `space-between-scripts` | `yes`: separate converted words from neighbouring words in another script (`葬送のフリーレン` → `葬送 no Furīren`) |
| `capitalize-scripts` | Scripts whose converted words start with a capital letter, e.g. `Katakana` |

**Sections**

- **`[to-latin]` (required).** Conversion rules.
- **`[from-latin]`.** Rules for going back.
- **`[after]`.** Replacements inside each converted word, applied once, left to right. The Japanese rules use them for doubled consonants and long vowels.

**Rules**

| Rule | Meaning |
|---|---|
| `г = h` | One letter |
| `зг = zgh` | Several letters; the longest match wins |
| `^є = ye` | Only at the start of a word |
| `ия$ = ia` | Only at the end of a word |
| `ь =` | Empty: the letter is dropped |

Matching ignores case, and the result follows the source: `Щука` → `Shchuka`, `ЩУКА` → `SHCHUKA`. Characters without a rule are left as they are.
