// Package tray provides the notification-area icon and the control menu.
package tray

import (
	"log"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/Spell6inder/selkit/internal/autostart"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/engine"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/icon"
	"github.com/Spell6inder/selkit/internal/settings"
	"github.com/Spell6inder/selkit/internal/win"
)

// How often to check whether the config file changed.
const configPoll = 2 * time.Second

// Run blocks until the menu exits.
func Run(e *engine.Engine, logPath, localeDir, version string) {
	systray.Run(func() { onReady(e, logPath, localeDir, version) }, func() { log.Printf("exit") })
}

// applyReq is a config from the settings window; it is applied in the tray goroutine, which owns configMod.
type applyReq struct {
	c   *config.Config
	res chan error
}

func onReady(e *engine.Engine, logPath, localeDir, version string) {
	setIcon := func(on bool) {
		if on {
			systray.SetIcon(icon.ICO())
		} else {
			systray.SetIcon(icon.DisabledICO())
		}
	}
	setIcon(e.Enabled())

	mEnabled := systray.AddMenuItemCheckbox(i18n.T("Enabled"), i18n.T("Catch selections and paste them with the middle button"), e.Enabled())
	mPrimary := systray.AddMenuItem(primaryTitle(e.Primary()), i18n.T("What the middle button will paste"))
	mPrimary.Disable()
	mClear := systray.AddMenuItem(i18n.T("Clear primary"), i18n.T("The middle button works as usual again until the next selection"))
	mHotkey := systray.AddMenuItemCheckbox(hotkeyTitle(e.Config()),
		i18n.T("Do not save every selection automatically, only with the combination"), e.Config().HotkeyCapture())
	mOSD := systray.AddMenuItemCheckbox(i18n.T("Show what got into primary"),
		i18n.T("A short hint near the cursor after every save"), e.Config().OSD != "off")
	mMenu := systray.AddMenuItemCheckbox(menuTitle(e.Config()),
		i18n.T("Keyboard layout, case, transliteration and more for the selected text"), e.Config().MenuModifier != "")
	systray.AddSeparator()
	mSettings := systray.AddMenuItem(i18n.T("Settings…"), config.Path())
	mAuto := systray.AddMenuItemCheckbox(i18n.T("Start with Windows"), "", autostart.Enabled())
	mLog := systray.AddMenuItem(i18n.T("Open log"), logPath)
	systray.AddSeparator()
	mQuit := systray.AddMenuItem(i18n.T("Exit"), "")

	setTooltip := func(s string) {
		tip := "Selkit"
		if s != "" {
			tip += "\n" + oneLine(s)
		}
		systray.SetTooltip(tip)
	}
	setTooltip(e.Primary())
	e.OnPrimary(func(s string) {
		mPrimary.SetTitle(primaryTitle(s))
		setTooltip(s)
	})

	// apply applies the config to the engine and the menu checkmarks.
	apply := func(c *config.Config) {
		e.SetConfig(c)
		e.SetEnabled(c.Enabled)
		setIcon(c.Enabled)
		setChecked(mEnabled, c.Enabled)
		setChecked(mHotkey, c.HotkeyCapture())
		setChecked(mOSD, c.OSD != "off")
		setChecked(mMenu, c.MenuModifier != "")
		mHotkey.SetTitle(hotkeyTitle(c))
		mMenu.SetTitle(menuTitle(c))
	}
	configMod := config.ModTime()
	reload := func() {
		c, err := config.Load()
		if err != nil {
			log.Printf("config: %v", err)
			win.MessageBox("Selkit", i18n.T("Error in the config file, keeping the previous settings:")+"\n"+err.Error())
			return
		}
		apply(c)
		log.Printf("config reloaded")
	}
	// save writes the config to the file and applies it.
	save := func(c *config.Config) error {
		err := config.Save(c)
		if err != nil {
			log.Printf("saving config: %v", err)
		}
		configMod = config.ModTime() // our own write; don't reload
		apply(c)
		return err
	}
	// change is a change from the tray menu: it first checks that it doesn't conflict with the other settings.
	change := func(f func(c *config.Config)) bool {
		c := *e.Config()
		f(&c)
		if err := c.Validate(); err != nil {
			win.MessageBox("Selkit", i18n.T("Could not save the settings:")+"\n"+err.Error())
			return false
		}
		if err := save(&c); err != nil {
			win.MessageBox("Selkit", i18n.T("Could not save the settings:")+"\n"+err.Error())
		}
		return true
	}

	applyCh := make(chan applyReq)
	openSettings := func() {
		settings.Show(settings.Options{
			Current: e.Config,
			Apply: func(c *config.Config) error {
				req := applyReq{c, make(chan error, 1)}
				applyCh <- req
				return <-req.res
			},
			OpenFile:  func() { open(config.Path()) },
			Languages: i18n.Languages(localeDir),
			Version:   version,
		})
	}

	go func() {
		poll := time.NewTicker(configPoll)
		defer poll.Stop()
		for {
			select {
			case <-poll.C:
				if m := config.ModTime(); !m.Equal(configMod) {
					configMod = m // even if the file is broken, don't show the error every time
					reload()
				}

			case req := <-applyCh:
				req.res <- save(req.c)
				log.Printf("settings saved from the window")

			case <-mEnabled.ClickedCh:
				on := !mEnabled.Checked()
				change(func(c *config.Config) { c.Enabled = on })
				log.Printf("enabled: %v", on)

			case <-mHotkey.ClickedCh:
				on := !mHotkey.Checked()
				if change(func(c *config.Config) {
					c.Capture = "auto"
					if on {
						c.Capture = "hotkey"
					}
				}) {
					log.Printf("save by combo: %v", on)
				}

			case <-mMenu.ClickedCh:
				on := !mMenu.Checked()
				if change(func(c *config.Config) {
					c.MenuModifier = ""
					if on {
						c.MenuModifier = lastMenuModifier
					}
				}) {
					log.Printf("transform menu: %v", on)
				}

			case <-mOSD.ClickedCh:
				on := !mOSD.Checked()
				change(func(c *config.Config) {
					switch {
					case !on:
						c.OSD = "off"
					case c.OSD == "off": // don't reset "always" from the config to "change"
						c.OSD = "change"
					}
				})
				log.Printf("cursor tooltip: %v", on)

			case <-mClear.ClickedCh:
				e.ClearPrimary()
				log.Printf("primary cleared")

			case <-mSettings.ClickedCh:
				go openSettings() // the window may wait on applyCh; don't block this goroutine

			case <-mAuto.ClickedCh:
				on := !mAuto.Checked()
				if err := autostart.Set(on); err != nil {
					log.Printf("autostart: %v", err)
					win.MessageBox("Selkit", i18n.T("Could not change autostart:")+"\n"+err.Error())
					continue
				}
				setChecked(mAuto, on)

			case <-mLog.ClickedCh:
				open(logPath)

			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func hotkeyTitle(c *config.Config) string {
	return i18n.Tf("Save only with %s+middle button", modName(c.CaptureModifier))
}

// The modifier the menu toggle restores when it is turned on again from the tray.
var lastMenuModifier = config.Default().MenuModifier

func menuTitle(c *config.Config) string {
	if c.MenuModifier != "" {
		lastMenuModifier = c.MenuModifier
	}
	return i18n.Tf("Text menu with %s+middle button", modName(lastMenuModifier))
}

func modName(m string) string {
	r := []rune(m)
	if len(r) > 0 {
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	}
	return string(r)
}

func setChecked(m *systray.MenuItem, on bool) {
	if on {
		m.Check()
	} else {
		m.Uncheck()
	}
}

func open(path string) {
	if err := win.ShellOpen(path); err != nil {
		log.Print(err)
	}
}

// primaryTitle is a single-line preview for the menu (& is a mnemonic in Windows menus, so it is doubled).
func primaryTitle(s string) string {
	if s == "" {
		return i18n.T("Primary: (empty)")
	}
	return "Primary: " + strings.ReplaceAll(oneLine(s), "&", "&&")
}

// oneLine is a short single-line preview of the text.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40]) + "…"
	}
	return s
}
