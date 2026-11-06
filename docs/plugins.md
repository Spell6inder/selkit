# Plugins

**English** · [Українська](uk/plugins.md)

A plugin adds your own items to the text menu. It is any program or script that reads the selected text on stdin and writes the result to stdout, so it can be written in any language. The same plugin works on Windows and Linux.

A ready example is [`examples/plugins/brackets`](../examples/plugins/brackets): it puts the text in brackets or takes them off, with a PowerShell script for Windows and a shell script for Linux.

## Built-in modules

Part of the menu is built into the program yet listed and turned off like a plugin:

| Module | What it does |
|---|---|
| `translit` | Latin script and back, by several systems ([transliteration](translit.md)) |
| `encoding` | URL and Base64: encode and decode |
| `quotes` | «Guillemets», “curly”, "straight" quotes and backticks, and taking quotes off |

They run inside the program, so they always show a preview at no cost. Their names are taken: a plugin folder with one of them is not loaded.

## Installing

**Settings → Plugins** has a line for where the plugin comes from: a folder, a zip file, a link to a zip file, or a GitHub repository as `github.com/owner/repo`. On Linux the same is a command:

```bash
selkit plugins install ./brackets
selkit plugins install brackets-1.0.zip
selkit plugins install https://example.com/brackets.zip
selkit plugins install github.com/owner/selkit-plugin-brackets
```

Before installing, Selkit shows the plugin: its name, version, source, the command it runs and its actions, and asks. In a script, `--yes` is the answer. The folder is named after the source: the folder or the zip file without its version, or the repository without a `selkit-plugin-` or `selkit-` prefix; `--name=ID` chooses another.

From GitHub, Selkit takes the newest release: a `.zip` attached to it, or else the source archive. It remembers where the plugin came from and which release it is, in `install.json` in the plugin folder, so it can be updated:

```bash
selkit plugins update
selkit plugins update brackets
selkit plugins remove brackets
```

**Check for updates** in the settings does the same. Nothing is downloaded unless you ask, and a plugin from a folder or a zip file has nothing to update from.

A plugin can also be copied into the plugins folder by hand:
- Windows: `%APPDATA%\selkit\plugins` (the **Plugins** tab of the settings opens it);
- Linux: `~/.config/selkit/plugins`.

The plugins are read each time the menu opens, so no restart is needed.

## Settings

**Settings → Plugins** lists the modules and plugins, with two checkboxes for each plugin:
- **the plugin name** turns it on or off;
- **Preview in menu** runs the plugin while the menu opens, to show its result next to the item and grey out the item when it does not fit. Without it the item is always available, and the plugin runs only when you choose it.

All plugins together get 0.3 s for the preview (0.5 s on Linux). Those that do not answer in time are shown without a preview. Turn the preview off for slow plugins, such as PowerShell scripts, which take about 0.3 s just to start.

On Linux the same settings are changed by a command; the name is the plugin's folder or the module's name:

```bash
selkit plugins
selkit plugins disable encoding
selkit plugins enable brackets
selkit plugins preview brackets off
```

They are stored in `config.json`, which can also be edited by hand:

```json
"plugins": {
  "brackets": {"preview": true},
  "encoding": {"disabled": true}
}
```

## Writing a plugin

A plugin is a folder with `plugin.json`:

```json
{
  "name": "Brackets",
  "description": "Put the text in brackets or take them off",
  "version": "1.0",
  "commands": {
    "windows": ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "brackets.ps1"],
    "linux": ["sh", "brackets.sh"]
  },
  "timeout_ms": 5000,
  "preview": false,
  "actions": [
    {"id": "round", "name": "(Round brackets)"},
    {"id": "unwrap", "name": "Remove brackets"}
  ]
}
```

| Key | Meaning |
|---|---|
| `name` | Name of the plugin in the settings and of its submenu |
| `description` | A line under the name in the settings; without it, the action names are shown |
| `version` | Shown when installing; a plugin from GitHub shows its release instead |
| `command` | The command line, the same on every system |
| `commands` | The command line per system (`windows`, `linux`), instead of `command` |
| `timeout_ms` | How long a run may take; the default is 2000 |
| `preview` | Whether the preview is on until the user changes it; the default is `true` |
| `actions` | Menu items: `id` (letters, digits, `-` and `_`) and `name` |

A plugin with one action is a single menu item; with several, it gets a submenu. If the program is in the plugin folder, it is run from there; otherwise it is looked up in `PATH`. The plugin folder is the working directory.

To publish a plugin on GitHub, make a release with the plugin folder in a `.zip`, `plugin.json` at its top or in one folder inside. Without such a file, the source archive of the release is used.

### Running

| | |
|---|---|
| stdin | The selected text, UTF-8 |
| stdout | The result, UTF-8. One trailing newline is dropped if the text had none, so `print` is fine |
| `SELKIT_ACTION` | The `id` of the chosen action |
| `SELKIT_LANG` | The interface language (`uk`, `en`) |
| `SELKIT_PREVIEW` | `1` when the result is only for the preview |
| exit code 0 | Done. If the result is the same as the text, the item is greyed out ("Nothing to change") |
| exit code 3 | The action does not fit this text: the item is greyed out, and stderr says why |
| other exit codes | An error; stderr is shown to the user |

A run that takes longer than `timeout_ms` is stopped.

**Windows PowerShell** reads a script without a BOM in the ANSI code page, and its console encoding is not UTF-8 by default. Start the script with:

```powershell
[Console]::InputEncoding = [Text.Encoding]::UTF8
[Console]::OutputEncoding = New-Object Text.UTF8Encoding $false
$text = [Console]::In.ReadToEnd()
```

Keep the script itself ASCII, or save it as UTF-8 with a BOM.

### Translation

The names in `plugin.json` are English. Translations are the plugin's own gettext files, `locales/<lang>.po` in its folder, where `msgid` is the English text from `plugin.json`:

```po
msgid "Remove brackets"
msgstr "Прибрати дужки"
```

Messages on stderr are the plugin's own: use `SELKIT_LANG` to translate them.

## Security

A plugin is an ordinary program with your rights, and it gets the selected text: install only plugins you trust. Selkit runs only what is in your plugins folder, downloads only when you install or update, and shows the command a plugin runs before installing it.
