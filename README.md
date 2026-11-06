# Selkit

**English** · [Українська](docs/uk/README.md)

Tools for selected text: on the middle mouse button on Windows, on a key on Linux.

- **Linux-style primary selection on Windows.** Select text anywhere, then middle-click to paste it. The regular clipboard stays as it was.
- **Text menu** for the selected text:
  - fix the keyboard layout (`ghbdsn` → `привіт`);
  - change case (camelCase, snake_case, UPPER…);
  - transliterate to Latin and back, with official systems and your own rules;
  - tidy spaces and lines;
  - encode or decode URL and Base64.
- **Plugins:** your own menu items, as scripts or programs in any language.
- **Repeat the last action** without opening the menu.

| | Windows 10/11 | Linux |
|---|---|---|
| Primary selection | ✔ | built into the system |
| Text menu | Shift + middle button | the `selkit menu` command on a key of your choice |
| Keyboard layout fix | ✔ | — |
| Interface | English, Ukrainian | English, Ukrainian |

## Install

Download from [Releases](https://github.com/Spell6inder/selkit/releases):
- **Windows:** `selkit.exe`. Put it anywhere and run it; it sits in the notification area.
- **Linux:** `selkit-*-linux-*.tar.gz`. See [Linux](docs/linux.md) for what else it needs.

Or build it with Go 1.27+:

```bash
go install -ldflags "-H=windowsgui" github.com/Spell6inder/selkit/cmd/selkit@latest
```

Drop `-ldflags "-H=windowsgui"` on Linux.

## Documentation

- [Windows](docs/windows.md): mouse combinations, the menu, settings, how the selection is read;
- [Linux](docs/linux.md): commands and the programs they use;
- [Transliteration rules](docs/translit.md): built-in systems and writing your own;
- [Plugins](docs/plugins.md): your own menu items, installing and writing them;
- [Translating](docs/translating.md) the interface;
- [Development](docs/development.md): building, tests, generated files.

## Privacy

Selkit reads the selected text only to paste or convert it. Nothing is sent anywhere, and there is no network code. The log does not contain the text unless you turn that on.

## License

[MIT](LICENSE) © 2026 Roman Hlushchak ([Spell6inder](https://spell6inder.pp.ua/))
