package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(c *Config)
		ok     bool
	}{
		{"default", func(c *Config) {}, true},
		{"hotkey", func(c *Config) { c.Capture = "hotkey" }, true},
		{"modifiers are case-insensitive", func(c *Config) {
			c.Capture, c.CaptureModifier, c.PasteModifier, c.MenuModifier, c.RepeatModifier = "hotkey", "Alt", "SHIFT", "Win", "CTRL"
		}, true},
		{"menu disabled", func(c *Config) { c.MenuModifier = "" }, true},
		{"unknown menu_modifier", func(c *Config) { c.MenuModifier = "caps" }, false},
		{"menu and paste with the same modifier", func(c *Config) { c.PasteModifier = "shift" }, false},
		{"menu and capture with the same modifier", func(c *Config) { c.Capture, c.MenuModifier = "hotkey", "ctrl" }, false},
		{"menu with ctrl in auto does not conflict", func(c *Config) { c.MenuModifier = "ctrl" }, true},
		{"repeat disabled", func(c *Config) { c.RepeatModifier = "" }, true},
		{"repeat and menu with the same modifier", func(c *Config) { c.RepeatModifier = "shift" }, false},
		{"repeat and capture with the same modifier", func(c *Config) { c.Capture, c.CaptureModifier = "hotkey", "alt" }, false},
		{"unknown mode", func(c *Config) { c.Mode = "clipboard" }, false},
		{"unknown capture", func(c *Config) { c.Capture = "manual" }, false},
		{"osd always", func(c *Config) { c.OSD = "always" }, true},
		{"unknown osd", func(c *Config) { c.OSD = "popup" }, false},
		{"empty capture_modifier", func(c *Config) { c.CaptureModifier = "" }, false},
		{"unknown paste_modifier", func(c *Config) { c.PasteModifier = "meta" }, false},
		{"same modifiers in hotkey", func(c *Config) { c.Capture, c.PasteModifier = "hotkey", "ctrl" }, false},
		{"same modifiers in auto do not conflict", func(c *Config) { c.PasteModifier = "ctrl" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.change(c)
			if err := c.validate(); (err == nil) != tt.ok {
				t.Fatalf("validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestHas(t *testing.T) {
	c := Default()
	if !c.CopyNeedsEdit("explorer.EXE") || !c.CopyNeedsEdit("excel.exe") {
		t.Fatal("process name comparison must be case-insensitive")
	}
	if c.IsTerminal("notepad.exe") {
		t.Fatal("notepad.exe is not a terminal")
	}
}

// Settings left under an old name of the program move to the new folder, with its log; an existing new folder wins.
func TestMigrateFrom(t *testing.T) {
	base := t.TempDir()
	t.Setenv("APPDATA", base)         // Windows
	t.Setenv("XDG_CONFIG_HOME", base) // Linux
	old := filepath.Join(base, "wheelkit")
	os.MkdirAll(filepath.Join(old, "plugins", "mine"), 0o755)
	os.WriteFile(filepath.Join(old, "config.json"), []byte(`{"language":"uk"}`), 0o644)
	os.WriteFile(filepath.Join(old, "wheelkit.log"), []byte("log"), 0o644)

	if moved, err := MigrateFrom("primeclip"); moved || err != nil {
		t.Errorf("from a name that was never used: %v, %v", moved, err)
	}
	moved, err := MigrateFrom("wheelkit")
	if !moved || err != nil {
		t.Fatalf("moved %v, %v", moved, err)
	}
	for _, p := range []string{"config.json", "selkit.log", filepath.Join("plugins", "mine")} {
		if _, err := os.Stat(filepath.Join(Dir(), p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the old folder is still there")
	}

	os.MkdirAll(old, 0o755)
	if moved, _ := MigrateFrom("wheelkit"); moved {
		t.Error("moved over settings that are already there")
	}
}
