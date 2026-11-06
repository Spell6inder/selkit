// Package config holds the Selkit settings in %APPDATA%\selkit\config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Spell6inder/selkit/internal/i18n"
)

type Config struct {
	Enabled bool `json:"enabled"`
	// Mode is how the selection is obtained: "uia", "copy" or "both" (UIA; if empty, Ctrl+C).
	Mode string `json:"mode"`
	// Capture is when to store the selection in primary: "auto" (after every selection gesture)
	// or "hotkey" (only on CaptureModifier + middle button, avoiding needless Ctrl+C).
	Capture         string `json:"capture"`
	CaptureModifier string `json:"capture_modifier"` // ctrl | shift | alt | win
	// PasteModifier is the modifier with which the middle button pastes ("" means no modifier).
	PasteModifier string `json:"paste_modifier"`
	// MenuModifier is the modifier with which the middle button opens the transform menu for the selected text
	// ("" disables the menu).
	MenuModifier string `json:"menu_modifier"`
	// RepeatModifier is the modifier with which the middle button immediately repeats the last menu action ("" disables it).
	RepeatModifier string `json:"repeat_modifier"`
	// Language is the interface language ("" means follow Windows; "en", "uk"… select the translation file of that name).
	Language string `json:"language"`
	// LogText writes the selected text itself to the log, not just its length.
	LogText bool `json:"log_text"`
	// OSD is a hint near the cursor: "off", "change" (when primary changes) or "always" (also after pasting).
	OSD           string `json:"osd"`
	OSDDurationMs int    `json:"osd_duration_ms"`
	// CleanHistory removes entries from the Win+V history that appeared because of our Ctrl+C.
	CleanHistory bool `json:"clean_history"`
	// KeyboardSelection catches selections made with the keyboard (Shift+arrows/Home/End, Ctrl+A).
	KeyboardSelection bool `json:"keyboard_selection"`

	SelectDelayMs int `json:"select_delay_ms"`
	CopyTimeoutMs int `json:"copy_timeout_ms"`
	// PasteTimeoutMs is how long to wait for the program to read the pasted text.
	PasteTimeoutMs int `json:"paste_timeout_ms"`
	// RestoreDelayMs is a fixed pause before restoring, if delayed rendering is unavailable.
	RestoreDelayMs int `json:"restore_delay_ms"`
	// UIATimeoutMs is how long to wait for the program via UI Automation (applied at startup).
	UIATimeoutMs int `json:"uia_timeout_ms"`

	// Terminals: Ctrl+C is an interrupt here, so do not copy; paste via Shift+Insert.
	Terminals []string `json:"terminals"`
	// PasteOnlyInEdit: the middle button has its own meaning, so paste only over an input field.
	PasteOnlyInEdit []string `json:"paste_only_in_edit"`
	// CopyOnlyInEdit: Ctrl+C copies something other than text (files etc.), so copy only from an input field.
	CopyOnlyInEdit []string `json:"copy_only_in_edit"`
	// Disabled: Selkit does nothing here.
	Disabled []string `json:"disabled"`

	// Plugins are the per-plugin settings, by plugin folder name; a plugin missing here is enabled.
	Plugins map[string]PluginSettings `json:"plugins,omitempty"`
}

func Default() *Config {
	return &Config{
		Enabled:           true,
		Mode:              "both",
		Capture:           "auto",
		CaptureModifier:   "ctrl",
		MenuModifier:      "shift",
		RepeatModifier:    "alt",
		CleanHistory:      true,
		OSD:               "off",
		OSDDurationMs:     1500,
		KeyboardSelection: true,
		SelectDelayMs:     60,
		CopyTimeoutMs:     150,
		PasteTimeoutMs:    1000,
		RestoreDelayMs:    150,
		UIATimeoutMs:      1000,
		Terminals: []string{
			"WindowsTerminal.exe", "conhost.exe", "OpenConsole.exe", "cmd.exe",
			"powershell.exe", "pwsh.exe", "mintty.exe", "alacritty.exe", "wezterm-gui.exe",
		},
		PasteOnlyInEdit: []string{
			"chrome.exe", "msedge.exe", "firefox.exe", "brave.exe", "opera.exe", "vivaldi.exe", "explorer.exe",
		},
		// Excel: a cell selection is not text, and Ctrl+C on a large range is heavy.
		CopyOnlyInEdit: []string{"explorer.exe", "EXCEL.EXE"},
		Disabled:       []string{},
	}
}

func Dir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "selkit")
}

// MigrateFrom moves settings and logs left by the program under an old name (wheelkit, primeclip),
// unless the new folder already exists. It reports whether anything was moved.
func MigrateFrom(oldName string) (bool, error) {
	old := filepath.Join(filepath.Dir(Dir()), oldName)
	if _, err := os.Stat(Dir()); err == nil {
		return false, nil
	}
	if _, err := os.Stat(old); err != nil {
		return false, nil
	}
	if err := os.Rename(old, Dir()); err != nil {
		return false, err
	}
	for _, suffix := range []string{".log", ".log.old"} {
		os.Rename(filepath.Join(Dir(), oldName+suffix), filepath.Join(Dir(), "selkit"+suffix))
	}
	return true, nil
}

func Path() string { return filepath.Join(Dir(), "config.json") }

// ModTime is the config file's last modification time (zero if there is no file).
func ModTime() time.Time {
	st, err := os.Stat(Path())
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// Load reads the config; if there is no file, it creates one with default values.
// Fields missing from the file are taken from Default.
func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		return c, Save(c)
	}
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	return c, nil
}

func (c *Config) validate() error {
	switch c.Mode {
	case "uia", "copy", "both":
	default:
		return errors.New(i18n.Tf("mode must be uia, copy or both, not %q", c.Mode))
	}
	switch c.Capture {
	case "auto", "hotkey":
	default:
		return errors.New(i18n.Tf("capture must be auto or hotkey, not %q", c.Capture))
	}
	switch c.OSD {
	case "off", "change", "always":
	default:
		return errors.New(i18n.Tf("osd must be off, change or always, not %q", c.OSD))
	}
	c.CaptureModifier = strings.ToLower(c.CaptureModifier)
	if !validModifier(c.CaptureModifier) || c.CaptureModifier == "" {
		return errors.New(i18n.Tf("capture_modifier must be ctrl, shift, alt or win, not %q", c.CaptureModifier))
	}
	// With each modifier the middle button does one thing, so the modifiers in use must not coincide.
	// paste_modifier counts even when empty (paste without a modifier); the rest only when enabled.
	used := map[string]string{}
	for _, m := range []struct {
		field string
		v     *string
		on    bool
	}{
		{"paste_modifier", &c.PasteModifier, true},
		{"capture_modifier", &c.CaptureModifier, c.HotkeyCapture()},
		{"menu_modifier", &c.MenuModifier, c.MenuModifier != ""},
		{"repeat_modifier", &c.RepeatModifier, c.RepeatModifier != ""},
	} {
		*m.v = strings.ToLower(*m.v)
		if !validModifier(*m.v) {
			return errors.New(i18n.Tf("%s must be empty, ctrl, shift, alt or win, not %q", m.field, *m.v))
		}
		if !m.on {
			continue
		}
		if other, dup := used[*m.v]; dup {
			return errors.New(i18n.Tf("%s and %s cannot use the same modifier (%s)", other, m.field, *m.v))
		}
		used[*m.v] = m.field
	}
	return nil
}

// Validate checks that the settings are consistent (the same code as during Load).
func (c *Config) Validate() error { return c.validate() }

func validModifier(m string) bool {
	switch m {
	case "", "ctrl", "shift", "alt", "win":
		return true
	}
	return false
}

func Save(c *Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), data, 0o644)
}

func (c *Config) IsTerminal(proc string) bool     { return has(c.Terminals, proc) }
func (c *Config) PasteNeedsEdit(proc string) bool { return has(c.PasteOnlyInEdit, proc) }
func (c *Config) CopyNeedsEdit(proc string) bool  { return has(c.CopyOnlyInEdit, proc) }
func (c *Config) IsDisabled(proc string) bool     { return has(c.Disabled, proc) }
func (c *Config) SelectDelay() time.Duration      { return ms(c.SelectDelayMs) }
func (c *Config) CopyTimeout() time.Duration      { return ms(c.CopyTimeoutMs) }
func (c *Config) RestoreDelay() time.Duration     { return ms(c.RestoreDelayMs) }
func (c *Config) PasteTimeout() time.Duration     { return ms(c.PasteTimeoutMs) }
func (c *Config) UIATimeout() time.Duration       { return ms(c.UIATimeoutMs) }
func (c *Config) OSDDuration() time.Duration      { return ms(c.OSDDurationMs) }
func (c *Config) HotkeyCapture() bool             { return c.Capture == "hotkey" }
func (c *Config) UseUIA() bool                    { return c.Mode == "uia" || c.Mode == "both" }
func (c *Config) UseCopy() bool                   { return c.Mode == "copy" || c.Mode == "both" }

func has(list []string, proc string) bool {
	for _, p := range list {
		if strings.EqualFold(p, proc) {
			return true
		}
	}
	return false
}

func ms(v int) time.Duration { return time.Duration(v) * time.Millisecond }

// PluginSettings is what the user chose for one plugin.
type PluginSettings struct {
	Disabled bool `json:"disabled,omitempty"`
	// Preview runs the plugin while the menu opens, to show its result next to the item; nil follows the plugin.
	Preview *bool `json:"preview,omitempty"`
}

// Plugin returns the settings of the plugin.
func (c *Config) Plugin(id string) PluginSettings { return c.Plugins[id] }

// SetPlugin changes the settings of the plugin. It copies the map, so a copy of the Config made before does not change.
func (c *Config) SetPlugin(id string, s PluginSettings) {
	m := make(map[string]PluginSettings, len(c.Plugins)+1)
	for k, v := range c.Plugins {
		m[k] = v
	}
	if s == (PluginSettings{}) {
		delete(m, id)
	} else {
		m[id] = s
	}
	c.Plugins = m
}

// ReadPlugins reads only the plugin settings from a config file; no file means none.
func ReadPlugins(path string) (map[string]PluginSettings, error) {
	raw, err := readRaw(path)
	if err != nil {
		return nil, err
	}
	var plugins map[string]PluginSettings
	if data, ok := raw[pluginsKey]; ok {
		if err := json.Unmarshal(data, &plugins); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return plugins, nil
}

// WritePlugin changes the settings of one plugin in a config file and leaves everything else in it alone,
// so that a file holding only plugin settings (Linux) does not fill up with the Windows ones.
// A broken file is reported, not replaced.
func WritePlugin(path, id string, s PluginSettings) error {
	raw, err := readRaw(path)
	if err != nil {
		return err
	}
	plugins, err := ReadPlugins(path)
	if err != nil {
		return err
	}
	c := Config{Plugins: plugins}
	c.SetPlugin(id, s)
	if len(c.Plugins) == 0 {
		delete(raw, pluginsKey)
	} else if raw[pluginsKey], err = json.Marshal(c.Plugins); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

const pluginsKey = "plugins"

func readRaw(path string) (map[string]json.RawMessage, error) {
	raw := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return raw, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return raw, nil
}
