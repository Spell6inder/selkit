# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-09

### Added

- Primary selection: paste the last selection with the middle mouse button without touching the clipboard. Selections are caught automatically or only with a modifier.
- Text menu on Shift + middle button:
  - keyboard layout fix, to the next layout or a chosen one;
  - case conversions and splitting identifiers into words;
  - transliteration to Latin and back by rule files: Ukrainian (KMU 2010), Bulgarian, Serbian, ISO 9, Japanese kana; user rules in `%APPDATA%\selkit\translit`;
  - line and space tools;
  - URL and Base64 encode/decode.
- Repeat the last menu action on Alt + middle button.
- In browsers and Explorer, combinations go to the program over links or when nothing is selected.
- Settings window with tabs; program lists can be filled from running programs.
- Optional hint near the cursor showing what got into primary.
- English and Ukrainian interface (gettext `.po`). User translations can be added in `%APPDATA%\selkit\locales`.
- Tray icon, which turns gray when disabled. The exe carries an icon, version info and a manifest.
- Plugins: your own menu items as programs or scripts in any language (stdin to stdout), with their own translations, a Plugins settings tab with a per-plugin preview switch, and an example in `examples/plugins`. Plugins are installed from a folder, a zip file, a link or a GitHub release, and updated from GitHub, in the settings or with `selkit plugins install`/`update`/`remove`.
- Built-in modules, listed and turned off like plugins: transliteration, encoding, and quotes («», “”, "", backticks, taking quotes off).
- The text menu checks the action that repeat would run (Linux: `↻`).
- Start with Windows.
- Settings, logs and the autostart entry left by the earlier names, primeclip and Wheelkit, move over on first start.
- Linux: `selkit menu` shows the text menu (without the layout fix) for the selection in rofi, fuzzel, wofi, dmenu, zenity or fzf and puts the result into the clipboard and primary; also `repeat`, `apply` as a stdin filter, `plugins` to list and configure plugins, and `shortcut` to show, assign and remove keys on KDE, Xfce and GNOME.

[Unreleased]: https://github.com/Spell6inder/selkit/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Spell6inder/selkit/releases/tag/v0.1.0
