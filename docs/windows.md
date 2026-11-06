# Selkit on Windows

**English** · [Українська](uk/windows.md)

Selkit runs on Windows 10/11 and sits in the notification area. To start it with Windows, use **Start with Windows** in the tray menu.

## Mouse

| Combination | Default | What it does |
|---|---|---|
| Middle button | — | Paste primary (the last selection) |
| Ctrl + middle | only in "save with a combination" mode | Save the selection to primary |
| Shift + middle | on | Text menu |
| Alt + middle | on | Repeat the last menu action |

Each modifier can be changed or turned off in **Settings → Mouse**. In browsers and Explorer a combination is passed to the program when the cursor is over a link or nothing is selected, so Ctrl+click and Shift+click on links keep working.

## Text menu

The menu works on the selected text and shows a preview of each result. Items that would change nothing are greyed out.

- **Keyboard layout.** Fixes text typed in the wrong layout (`ghbdsn` → `привіт`), cycling through the installed layouts. With more than two layouts, **Layout to** picks one.
- **To Latin / From Latin.** Official Ukrainian, Bulgarian and Serbian systems, ISO 9 for any Cyrillic, Japanese kana, plus your own rules. When several systems fit, **Latin by** lists them. See [Transliteration rules](translit.md).
- **Case.** camelCase, PascalCase, snake_case, SCREAMING_SNAKE, kebab-case, dot.case, separate words, UPPERCASE, lowercase, Title Case, Sentence case, iNVERT cASE.
- **Spaces and lines.** Collapse spaces, join into one line, sort, remove duplicates, reverse the order.
- **Encoding.** URL and Base64, both ways.
- **To primary.** Put the selection into primary without changing it.
- **Plugins.** Your own items, see [Plugins](plugins.md). **Settings → Plugins** turns each plugin and its preview on or off.

The result replaces the selection. Where the text cannot be edited, it goes to the clipboard instead. **Repeat** (and Alt + middle button) runs the last action again on the current selection: the layout keeps cycling, and Latin goes back and forth.

## How it works

- **Reading the selection.** Selkit first asks the program through UI Automation. If that does not work, it sends Ctrl+C and then restores the clipboard. Copies made this way are removed from the Win+V history.
- **Terminals.** Selkit never sends Ctrl+C to a terminal, where it would interrupt the running command. It pastes with Shift+Insert.
- **Per-program lists** in **Settings → Programs**. Each list can be filled from the running programs:
  - terminals;
  - programs where Selkit is disabled;
  - programs where it pastes or copies only inside input fields.

## Settings and files

Everything is in `%APPDATA%\selkit`:

| Path | What it is |
|---|---|
| `config.json` | Settings. The settings window edits it; it can also be edited by hand, and changes are picked up on the fly |
| `selkit.log` | The log: which program and method were used. The text itself is written only with **Write the selected text to the log** on |
| `translit\*.rules` | Your own [transliteration rules](translit.md) |
| `locales\*.po` | Your own [translations](translating.md) |
| `plugins\` | [Plugins](plugins.md), one folder each |

The **About** tab opens the program folder and the settings folder.

## Antivirus programs

Selkit installs a global mouse and keyboard hook and may send Ctrl+C. Some antivirus programs treat that as suspicious; the source is here to check.
