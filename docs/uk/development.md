# Розробка Selkit

[English](../development.md) · **Українська**

Потрібен Go 1.27+.

```bash
go build -ldflags "-H=windowsgui" -o var/bin/selkit.exe ./cmd/selkit
go test ./...
```

Для Linux — `GOOS=linux go build ./cmd/selkit`. [`scripts/linux-check.sh`](../../scripts/linux-check.sh) запускає цю збірку в контейнері зі справжніми програмами: rofi під X11, fzf у терміналі, fuzzel під Wayland; команди наведено на початку скрипта. Linux-частина — це `internal/cli`. Спільні з Windows дії меню — `internal/actions`, поверх `internal/textops`, `internal/translit` і `internal/plugin`.

- **Згенеровані файли комітяться**, щоб звичайний `go build` працював:
  - `resources/icon/*.ico` і `cmd/selkit/rsrc_windows_amd64.syso` містять іконку, версію файла й маніфест; `go generate ./cmd/selkit` перебудовує їх з `resources/icon/*.svg`. Ці два файли своєю чергою робить `go run ./internal/icon/mark LiberationSans-Bold.ttf resources/icon`: він малює літери контурами цього шрифту й центрує знак;
  - `resources/locales/selkit.pot`: `SELKIT_UPDATE_POT=1 go test ./internal/i18n`.

  Якщо ці файли застаріли, тести падають.
- **Інтерактивні тести** відкривають справжні вікна, тож без змінної середовища пропускаються: `SELKIT_SETTINGS_TEST`, `SELKIT_MENU_TEST`, `SELKIT_OSD_TEST`, `SELKIT_UI_TEST`, `SELKIT_CLIPBOARD_TEST`, `SELKIT_ICON_PREVIEW`. `SELKIT_MENU_TEXT` задає текст для тесту меню, а `SELKIT_SETTINGS_SHOTS=ТЕКА` зберігає знімок кожної вкладки налаштувань.
- **Код і коментарі англійською.** Тексти для користувача проходять через `i18n.T` та споріднені функції.
- **Релізи.** Тег `v*` запускає `.github/workflows/release.yml`: він збирає exe для Windows і архіви для Linux, а примітки бере з `CHANGELOG.md`.
