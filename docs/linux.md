# Selkit on Linux

**English** · [Українська](uk/linux.md)

Linux already has primary selection, so there Selkit is only the text menu: a command to bind to a key or mouse button in your desktop settings.

```bash
selkit menu
```

It takes the selected text, shows the actions with a preview in a menu program, and puts the result into both the clipboard and primary. Paste it with Ctrl+V or the middle button.

The menu has the same actions as on [Windows](windows.md#text-menu), [plugins](plugins.md) included, except the keyboard layout fix, which needs the Windows layout tables. There are no mouse combinations either: the desktop runs the command.

## Commands

| Command | What it does |
|---|---|
| `selkit menu` | The text menu for the selection |
| `selkit repeat` | Run the action last chosen in the menu, without the menu |
| `selkit apply ID` | Convert stdin to stdout: `echo Щука \| selkit apply translit` |
| `selkit list` | List the action IDs (`translit`, `translit:uk`, `snake`, `base64-encode`…) |
| `selkit plugins` | List, install, update and remove [plugins](plugins.md), or change their settings |
| `selkit shortcut` | Show, assign and remove the keys of the menu and of repeat ([below](#key-bindings)) |
| `selkit version` | Show the version |

Options of `menu` and `repeat`:

| Option | What it does |
|---|---|
| `-type` | Also type the result over the selection |
| `-clipboard` | Take the clipboard instead of the selection |
| `-picker NAME` | The menu program to use, instead of the usual one: `fuzzel` on Wayland, `rofi` on X11 (`menu` only) |

`menu` and `repeat` take text from a pipe or a file instead of the selection. The result then also goes to stdout, and nothing is typed:

```bash
echo 'test foo bar' | selkit menu
selkit repeat < notes.txt > result.txt
```

## Key bindings

Assign the usual keys, `Ctrl+Shift+Alt+X` for the menu and `Ctrl+Shift+Alt+Z` for repeat, or your own; one of them can be left out:

```bash
selkit shortcut set --default
selkit shortcut set --menu=F9 --repeat=F7
```

`set` also installs the icon in `~/.local/share/icons`, for the settings, notifications and the search line of the `rofi` menu; removing both keys removes it.

The result replaces the selection; with `--no-type` it only goes to the clipboard. `--clipboard` takes the clipboard instead of the selection, and `--picker=NAME` chooses the menu program. If a key is taken, nothing changes.

See what is assigned, or open the keyboard settings of the desktop:

```bash
selkit shortcut status
selkit shortcut open
```

Before deleting the program, take the keys away. Settings, rules and plugins stay in `~/.config/selkit`; delete that folder to remove them too. With `--menu` or `--repeat`, only that key goes.

```bash
selkit shortcut remove
```

A key is modifiers (`Ctrl`, `Alt`, `Shift`, `Meta`) and a key name, joined by `+`. Selkit gives each of `menu` and `repeat` one key, so assigning another moves it, and a key already used by something else is refused.

In KDE both are one group, **Selkit**, in the shortcut settings: **Launch** is the menu. Keys you add there yourself stay when Selkit changes its own; `remove` takes them away too.

| Desktop | `selkit shortcut set` |
|---|---|
| KDE Plasma (X11 and Wayland) | Assigns the key; also needs `dbus-send` and `kbuildsycoca6` |
| Xfce | Assigns the key, through `xfconf-query` |
| Sway, i3, Hyprland | Prints the line to add to your configuration |
| GNOME | Assigns the key, through `gsettings`; not yet tried on a real desktop |
| Others | Prints the command, for the keyboard settings of the desktop |

## Programs it uses

Selkit talks to the desktop through the usual command-line tools, so it needs no libraries:

| For | Wayland | X11 |
|---|---|---|
| Selection and clipboard | `wl-clipboard` | `xclip` or `xsel` |
| Menu | `fuzzel`, `wofi`, or any from the X11 column (on GNOME not `fuzzel` or `wofi`) | `rofi`, `bemenu`, `dmenu` or `zenity` |
| Typing (`-type`) | `wtype` or `ydotool` | `xdotool` |
| Notifications | `notify-send` | `notify-send` |

When the command runs in a terminal, `fzf` is the menu if it is installed. Without `notify-send`, messages go to the terminal.

The menu is one list with the group names as headers. In `rofi` a header cannot be chosen; in the other programs, choosing one shows the menu again. `↻` marks the action that `repeat` would run; `rofi` also highlights it.

Not every Wayland compositor supports primary selection; without it, use `-clipboard`.

On Wayland, `-type` tries `wtype` first. KDE does not support it, so there the result is pasted with Shift+Insert, through `ydotool` (which needs `ydotoold` and `/dev/uinput`) or `xdotool`. With `xdotool`, Plasma asks once per session to allow control of input devices. If nothing works, the result stays in the clipboard.

## Files

Everything is in `~/.config/selkit`:

| Path | What it is |
|---|---|
| `translit/*.rules` | Your own [transliteration rules](translit.md) |
| `locales/*.po` | Your own [translations](translating.md) |
| `plugins/` | [Plugins](plugins.md), one folder each |
| `config.json` | Only `language` (`"uk"`, `"en"`; without it, the system locale) and `plugins` are used on Linux |
| `last-action` | The action for `repeat` |
