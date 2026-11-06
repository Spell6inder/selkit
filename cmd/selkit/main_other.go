//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/cli"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/translit"
)

// Without the Windows hooks, Selkit is a command for a key binding: see internal/cli.
func main() {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("Selkit", version)
		return
	}
	// The program used to be called Wheelkit: its settings, rules and plugins move over.
	if _, err := config.MigrateFrom("wheelkit"); err != nil {
		fmt.Fprintln(os.Stderr, "selkit: moving the wheelkit settings:", err)
	}
	dir := config.Dir()
	// Of config.json, the language and the plugin settings matter here; Load would create the file,
	// so it is read only if present.
	cfg := config.Default()
	if _, err := os.Stat(config.Path()); err == nil {
		if c, err := config.Load(); err == nil {
			cfg = c
		} else {
			fmt.Fprintln(os.Stderr, "selkit:", err)
		}
	}
	i18n.Init(cfg.Language, filepath.Join(dir, "locales"))
	if err := translit.Init(filepath.Join(dir, "translit")); err != nil {
		fmt.Fprintln(os.Stderr, "selkit: transliteration rules:", err)
	}
	set, err := actions.Load(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "selkit:", err)
	}
	os.Exit(cli.Run(os.Args[1:], set, dir, os.Stdin, os.Stdout, os.Stderr))
}
