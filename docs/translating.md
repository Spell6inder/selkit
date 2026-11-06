# Translating Selkit

**English** · [Українська](uk/translating.md)

Translations are gettext `.po` files in [`resources/locales`](../resources/locales). Start from the template `selkit.pot` with [Poedit](https://poedit.net/) or any translation platform.

You can try a translation without building: put `xx.po` into `%APPDATA%\selkit\locales` (on Linux, `~/.config/selkit/locales`) and restart Selkit. A file there also overrides the built-in translation, and strings missing from it stay in English.

To ship a translation, add the file to `resources/locales`. The tests check that it covers every string and keeps the same `%` placeholders.
