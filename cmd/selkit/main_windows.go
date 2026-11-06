package main

//go:generate go run github.com/Spell6inder/selkit/internal/icon/generate

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/Spell6inder/selkit/internal/autostart"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/engine"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/translit"
	"github.com/Spell6inder/selkit/internal/tray"
	"github.com/Spell6inder/selkit/internal/win"
)

const maxLogSize = 1 << 20

func main() {
	// Until the config is read, use the Windows language; the user's own translations live next to the config.
	localeDir := filepath.Join(config.Dir(), "locales")
	i18n.Init("", localeDir)

	if !win.SingleInstance(`Local\selkit`) {
		win.MessageBox("Selkit", i18n.T("Selkit is already running — see the icon in the notification area."))
		return
	}

	// The program used to be called Wheelkit, and before that primeclip: carry over the settings and
	// autostart entry, from the newer name first.
	var moved bool
	var migrateErr error
	var movedFrom string
	for _, old := range oldNames {
		if moved, migrateErr = config.MigrateFrom(old); moved || migrateErr != nil {
			movedFrom = old
			break
		}
	}
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		win.MessageBox("Selkit", i18n.Tf("Could not create %s:", config.Dir())+"\n"+err.Error())
		return
	}
	logPath := filepath.Join(config.Dir(), "selkit.log")
	setupLog(logPath)
	if migrateErr != nil {
		log.Printf("moving old settings from %s: %v", movedFrom, migrateErr)
	} else if moved {
		log.Printf("settings moved from the %s folder", movedFrom)
	}
	for _, old := range oldNames {
		if err := autostart.MigrateFrom(old); err != nil {
			log.Printf("autostart migration from %s: %v", old, err)
		}
	}
	if err := win.SetDPIAware(); err != nil {
		log.Printf("DPI: %v; checks under the cursor may miss on scaled monitors", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v; using defaults", err)
		win.MessageBox("Selkit", i18n.T("Error in the config file, using the default settings:")+"\n"+err.Error())
		cfg = config.Default()
	}
	lang, err := i18n.Init(cfg.Language, localeDir)
	if err != nil {
		log.Printf("translation: %v", err)
		win.MessageBox("Selkit", i18n.T("Error in the translation file:")+"\n"+err.Error())
	}

	if err := translit.Init(filepath.Join(config.Dir(), "translit")); err != nil {
		log.Printf("transliteration rules: %v", err)
	}
	e := engine.New(cfg)
	if err := e.Start(); err != nil {
		log.Printf("start: %v", err)
		win.MessageBox("Selkit", i18n.T("Could not install the mouse hook:")+"\n"+err.Error())
		return
	}
	log.Printf("started %s (mode=%s, enabled=%v, lang=%s)", version, cfg.Mode, cfg.Enabled, lang)

	tray.Run(e, logPath, localeDir, version)
}

// setupLog writes to a file (rotating to a single .old) and, if there is a console, to stderr.
func setupLog(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogSize {
		os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}

// oldNames are the names the program had, newest first.
var oldNames = []string{"wheelkit", "primeclip"}
