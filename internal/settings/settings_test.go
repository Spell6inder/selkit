package settings

import (
	"os"
	"testing"
	"time"

	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
)

var procPostMessage = user32.NewProc("PostMessageW")

// Opens the window and shows each tab for 1.5 s (to take screenshots), then closes it.
// Runs only when requested explicitly: SELKIT_SETTINGS_TEST=1 go test ./internal/settings
func TestWindowVisual(t *testing.T) {
	if os.Getenv("SELKIT_SETTINGS_TEST") == "" {
		t.Skip("SELKIT_SETTINGS_TEST not set")
	}
	i18n.Init("uk", "")
	Show(Options{
		Current:   config.Default,
		Apply:     func(*config.Config) error { return nil },
		Languages: i18n.Languages(""),
		Version:   "test",
	})
	mu.Lock()
	hwnd := open
	mu.Unlock()
	w := owner(hwnd)
	if w == nil {
		t.Fatal("window was not created")
	}
	for i := range w.pages {
		w.selectPage(i)
		time.Sleep(1500 * time.Millisecond)
		if shotDir() != "" {
			if err := shot(hwnd, shotPath(i)); err != nil {
				t.Error(err)
			}
		}
	}
	if progs := runningPrograms(); len(progs) == 0 {
		t.Error("no running program with a window found")
	} else {
		t.Logf("running: %d, e.g. %v", len(progs), progs[0])
	}
	procPostMessage.Call(hwnd, wmClose, 0, 0)
	time.Sleep(300 * time.Millisecond)
}
