# Developing Selkit

**English** · [Українська](uk/development.md)

Requires Go 1.27+.

```bash
go build -ldflags "-H=windowsgui" -o var/bin/selkit.exe ./cmd/selkit
go test ./...
```

For Linux, `GOOS=linux go build ./cmd/selkit`. [`scripts/linux-check.sh`](../scripts/linux-check.sh) runs that build in a container with real programs: rofi on X11, fzf in a terminal, fuzzel on Wayland; the commands are at the top of the script. The Linux part is `internal/cli`. The menu actions shared with Windows are `internal/actions`, on top of `internal/textops`, `internal/translit` and `internal/plugin`.

- **Generated files are committed**, so a plain `go build` works:
  - `resources/icon/*.ico` and `cmd/selkit/rsrc_windows_amd64.syso` hold the icon, file version and manifest; `go generate ./cmd/selkit` rebuilds them from `resources/icon/*.svg`. Those two are made in turn by `go run ./internal/icon/mark LiberationSans-Bold.ttf resources/icon`, which draws the letters as outlines of that font and centres the mark;
  - `resources/locales/selkit.pot`: `SELKIT_UPDATE_POT=1 go test ./internal/i18n`.

  Tests fail when these are out of date.
- **Interactive tests** open real windows and are skipped unless an environment variable is set: `SELKIT_SETTINGS_TEST`, `SELKIT_MENU_TEST`, `SELKIT_OSD_TEST`, `SELKIT_UI_TEST`, `SELKIT_CLIPBOARD_TEST`, `SELKIT_ICON_PREVIEW`. `SELKIT_MENU_TEXT` sets the text for the menu test, and `SELKIT_SETTINGS_SHOTS=DIR` saves a picture of each settings tab.
- **Code and comments are in English.** User-facing strings go through `i18n.T` and friends.
- **Releases.** Pushing a `v*` tag runs `.github/workflows/release.yml`: it builds the Windows exe and the Linux archives and takes the notes from `CHANGELOG.md`.
