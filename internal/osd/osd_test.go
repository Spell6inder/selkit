package osd

import (
	"os"
	"testing"
	"time"

	"github.com/Spell6inder/selkit/internal/win"
)

// Visual check: shows the tooltip on screen at point (300, 300) for 4 s.
// Run: SELKIT_OSD_TEST=1 go test -run TestShowVisual ./internal/osd
func TestShowVisual(t *testing.T) {
	if os.Getenv("SELKIT_OSD_TEST") == "" {
		t.Skip("SELKIT_OSD_TEST not set")
	}
	win.SetDPIAware()
	o, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	text := "func (e *Engine) paste(cfg *config.Config, text, proc string) (note string, ok bool) {\n\twin.SendMouse(win.MouseLeftDown, win.MouseLeftUp)\n\ttime.Sleep(40 * time.Millisecond)\n\n\tsaved, ok := e.saveClipboard(proc)\n}"
	o.Show(Title("Primary", text), Preview(text), win.Point{X: 300, Y: 300}, 4*time.Second)
	time.Sleep(4500 * time.Millisecond)
}
