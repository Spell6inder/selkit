// Package engine is the heart of Selkit: mouse hook, selection detection, middle-click paste.
//
// The hook runs on its own OS thread and only forwards events; all the work (UIA, clipboard, SendInput)
// happens on a single worker thread with COM initialized.
package engine

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Spell6inder/selkit/internal/clip"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/history"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/menu"
	"github.com/Spell6inder/selkit/internal/osd"
	"github.com/Spell6inder/selkit/internal/uia"
	"github.com/Spell6inder/selkit/internal/win"
)

// Class of the hidden xterm.js textarea (terminals in Electron): Ctrl+C without a selection there is SIGINT.
const xtermClass = "xterm-helper-textarea"

// After this many consecutive selections where UIA had no TextPattern but Ctrl+C worked,
// go straight to Ctrl+C for this app (until restart).
const noUIALimit = 3

// Saving the clipboard for longer than this is logged (delayed rendering in heavy apps).
const slowSave = 100 * time.Millisecond

type mouseEvent struct {
	msg  uintptr
	pt   win.Point
	time uint32
}

// State of the intercepted middle button, shared by the hook and the worker.
const (
	midIdle    = iota
	midPending // down swallowed, decision not made yet
	midForward // down passed on; the real up is passed too
	midSwallow // pasting; the real up is swallowed
)

type Engine struct {
	cfg        atomic.Pointer[config.Config]
	enabled    atomic.Bool
	hasPrimary atomic.Bool
	events     chan mouseEvent

	lazy    *clip.Lazy       // nil: paste with a fixed delay
	history *history.Cleaner // nil: Win+V history API unavailable
	osd     *osd.OSD         // nil: the tooltip could not be created
	menu    *menu.Menu       // nil: transform menu unavailable

	// Worker thread only.
	uia   *uia.Client
	noUIA map[string]int // process -> consecutive times UIA had no TextPattern but Ctrl+C worked
	last  *action        // last menu action, for repeat

	// Last event the hook saw, for the watchdog that checks the hook is still alive.
	lastHookPt atomic.Uint64
	lastHookAt atomic.Int64
	hookDead   bool    // hook thread only
	kb         kbState // hook thread only

	mid struct {
		sync.Mutex
		state  int
		upSeen bool
	}

	mu        sync.Mutex
	primary   string
	onPrimary func(string)
}

func New(cfg *config.Config) *Engine {
	e := &Engine{events: make(chan mouseEvent, 256), noUIA: map[string]int{}}
	e.cfg.Store(cfg)
	e.enabled.Store(cfg.Enabled)
	return e
}

func (e *Engine) Config() *config.Config     { return e.cfg.Load() }
func (e *Engine) SetConfig(c *config.Config) { e.cfg.Store(c) }
func (e *Engine) Enabled() bool              { return e.enabled.Load() }
func (e *Engine) SetEnabled(on bool)         { e.enabled.Store(on) }

func (e *Engine) Primary() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.primary
}

// OnPrimary sets a callback invoked from the worker thread on every primary change.
func (e *Engine) OnPrimary(f func(string)) {
	e.mu.Lock()
	e.onPrimary = f
	e.mu.Unlock()
}

func (e *Engine) ClearPrimary() { e.setPrimary("") }

// showOSD shows a tooltip with the text near the cursor if it is enabled for this event.
func (e *Engine) showOSD(cfg *config.Config, action, text string, onPaste bool) {
	if e.osd == nil || cfg.OSD == "off" || (onPaste && cfg.OSD != "always") {
		return
	}
	e.osd.Show(osd.Title(action, text), osd.Preview(text), win.CursorPos(), cfg.OSDDuration())
}

func (e *Engine) setPrimary(s string) {
	e.mu.Lock()
	e.primary = s
	f := e.onPrimary
	e.mu.Unlock()
	e.hasPrimary.Store(s != "")
	if f != nil {
		f(s)
	}
}

// Start launches the worker and installs the hook; it returns an error if the hook could not be installed.
func (e *Engine) Start() error {
	if l, err := clip.StartLazy(); err != nil {
		log.Printf("delayed rendering unavailable (%v); pasting with a fixed delay", err)
	} else {
		e.lazy = l
	}
	if h, err := history.Start(); err != nil {
		log.Printf("Win+V history cleanup unavailable: %v", err)
	} else {
		e.history = h
	}
	if o, err := osd.Start(); err != nil {
		log.Printf("cursor tooltip unavailable: %v", err)
	} else {
		e.osd = o
	}
	if m, err := menu.Start(); err != nil {
		log.Printf("transform menu unavailable: %v", err)
	} else {
		e.menu = m
	}
	go e.worker()
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := win.SetHooks(e.mouseProc, e.keyboardProc); err != nil {
			errc <- err
			return
		}
		e.markHookAlive(win.CursorPos())
		errc <- nil
		win.StartThreadTimer(watchdogEvery)
		win.RunMessageLoop(e.watchdog)
	}()
	return <-errc
}

const (
	watchdogEvery   = 5000 // ms
	hookSilentLimit = 3 * time.Second
)

func (e *Engine) markHookAlive(pt win.Point) {
	e.lastHookPt.Store(uint64(pt.Packed()))
	e.lastHookAt.Store(time.Now().UnixNano())
}

// watchdog: Windows silently removes the hook if the callback ever exceeds LowLevelHooksTimeout.
// The sign is a moved cursor while the hook has seen no events for a long time; then the hook is reinstalled.
// False positives (SetCursorPos, secure desktop) are harmless, just an extra reinstall.
func (e *Engine) watchdog() {
	cur := win.CursorPos()
	silent := time.Since(time.Unix(0, e.lastHookAt.Load()))
	if uint64(cur.Packed()) == e.lastHookPt.Load() || silent < hookSilentLimit {
		e.hookDead = false
		return
	}
	if err := win.ReinstallHooks(); err != nil {
		log.Printf("watchdog: failed to reinstall hooks: %v", err)
		return
	}
	if !e.hookDead { // don't flood the log while, for example, the lock screen is open
		log.Printf("watchdog: mouse hook silent for %v despite cursor movement; hooks reinstalled", silent.Round(time.Second))
	}
	e.hookDead = true
	e.markHookAlive(cur)
}

// mouseProc must return immediately or Windows removes the hook, so it only forwards the event.
// Our own (injected) events pass through unchanged.
func (e *Engine) mouseProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 {
		h := win.ParseMouseHook(lParam)
		e.markHookAlive(h.Pt)
		if !h.Injected {
			ev := mouseEvent{wParam, h.Pt, h.Time}
			switch wParam {
			case win.WMLButtonDown, win.WMLButtonUp:
				if e.enabled.Load() {
					e.post(ev)
				}
			case win.WMMButtonDown:
				// We could neither paste into an elevated window nor pass the click back, so leave it alone.
				// Without primary there is nothing to intercept, except in save-by-combo mode, for the menu or repeat.
				if e.enabled.Load() && (e.hasPrimary.Load() || e.cfg.Load().HotkeyCapture() || e.comboHeld()) &&
					!win.ForeignElevated(win.WindowAt(h.Pt)) {
					e.mid.Lock()
					e.mid.state, e.mid.upSeen = midPending, false
					e.mid.Unlock()
					if e.post(ev) {
						return 1
					}
					e.mid.Lock()
					e.mid.state = midIdle
					e.mid.Unlock()
				}
			case win.WMMButtonUp:
				// Handle regardless of enabled so an up is not lost after a swallowed down.
				e.mid.Lock()
				swallow := false
				switch e.mid.state {
				case midPending:
					e.mid.upSeen, swallow = true, true
				case midSwallow:
					e.mid.state, swallow = midIdle, true
				case midForward:
					e.mid.state = midIdle
				}
				e.mid.Unlock()
				if swallow {
					return 1
				}
			}
		}
	}
	return win.CallNextHook(nCode, wParam, lParam)
}

func (e *Engine) post(ev mouseEvent) bool {
	select {
	case e.events <- ev:
		return true
	default:
		return false
	}
}

func (e *Engine) worker() {
	runtime.LockOSThread()
	uia.InitThread()
	if c, note, err := uia.New(e.cfg.Load().UIATimeout()); err != nil {
		log.Printf("UI Automation unavailable (%v); only Ctrl+C will work", err)
	} else {
		e.uia = c
		log.Print(note)
	}

	dblTime := win.DoubleClickTime()
	dblCx, dblCy := win.Metric(win.SMCxDoubleClk)/2, win.Metric(win.SMCyDoubleClk)/2
	dragCx, dragCy := win.Metric(win.SMCxDrag), win.Metric(win.SMCyDrag)

	var (
		downPt     win.Point
		downRoot   uintptr
		downRect   win.Rect
		lastDownPt win.Point
		lastDownT  uint32
		clicks     int
	)

	for ev := range e.events {
		switch ev.msg {
		case win.WMLButtonDown:
			if clicks > 0 && ev.time-lastDownT <= dblTime &&
				abs(ev.pt.X-lastDownPt.X) <= dblCx && abs(ev.pt.Y-lastDownPt.Y) <= dblCy {
				clicks++
			} else {
				clicks = 1
			}
			downPt, lastDownPt, lastDownT = ev.pt, ev.pt, ev.time
			downRoot = win.RootWindowAt(ev.pt)
			downRect = win.WindowRect(downRoot)

		case win.WMLButtonUp:
			if e.cfg.Load().HotkeyCapture() {
				continue // selections are saved only via the combo
			}
			switch {
			case abs(ev.pt.X-downPt.X) > dragCx || abs(ev.pt.Y-downPt.Y) > dragCy:
				if win.WindowRect(downRoot) != downRect {
					continue // window was moved or resized, not a selection
				}
				if ct, ok := e.uia.DragHandleAt(downPt); ok {
					log.Printf("[%s] drag         skipped: dragged element type=%d", win.ForegroundProcess(), ct)
					continue
				}
				e.onSelection("drag", ev.pt)
			case clicks == 2:
				e.onSelection("double-click", ev.pt)
			case clicks >= 3:
				e.onSelection("triple-click", ev.pt)
			case win.KeyDown(win.VKShift):
				e.onSelection("shift-click", ev.pt)
			}

		case win.WMMButtonDown:
			e.onMiddle(ev.pt)

		case evKeySelect:
			e.onSelection("keyboard", ev.pt)

		case evSelectAll:
			e.onSelection("select-all", ev.pt)
		}
	}
}

// gestureHotkey is an explicit user command (combo + middle button), not a guess from a gesture.
const gestureHotkey = "hotkey"

// surelySelects lists gestures after which a terminal almost certainly has a selection.
func surelySelects(gesture string) bool {
	switch gesture {
	case "drag", "double-click", "triple-click", gestureHotkey, gestureMenu, gestureRepeat:
		return true
	}
	return false
}

func (e *Engine) onSelection(gesture string, pt win.Point) {
	cfg := e.cfg.Load()
	if cfg.HotkeyCapture() && gesture != gestureHotkey {
		return
	}
	s, ok := e.grabSelection(cfg, gesture, pt)
	if !ok {
		return
	}
	switch {
	case s.text == "":
		log.Printf("[%s] %-12s ✘ (%s, %v)", s.proc, gesture, s.how, s.took)
	case strings.TrimSpace(s.text) == "":
		log.Printf("[%s] %-12s ignored: whitespace only (%s, %v)", s.proc, gesture, s.how, s.took)
	default:
		e.setPrimary(s.text)
		log.Printf("[%s] %-12s ✔ %s (%s, %v)", s.proc, gesture, describeText(cfg, s.text), s.how, s.took)
		e.showOSD(cfg, "Primary", s.text, false)
	}
}

// selection is the selected text and where it came from.
type selection struct {
	text, how   string
	proc        string
	fg          uintptr
	focusedEdit bool
	took        time.Duration
}

// grabSelection gets the selected text via UIA, or via Ctrl+C if that is empty.
// ok=false means it is not allowed or not worth even trying (the reason is already logged).
func (e *Engine) grabSelection(cfg *config.Config, gesture string, pt win.Point) (s selection, ok bool) {
	time.Sleep(cfg.SelectDelay()) // give the app a moment to update the selection
	fg := win.ForegroundWindow()
	proc := win.ProcessOf(fg)
	if cfg.IsDisabled(proc) {
		return s, false
	}
	if win.ForeignElevated(fg) {
		log.Printf("[%s] %-12s skipped: window is elevated", proc, gesture)
		return s, false
	}

	focusedEdit, focusedClass := e.uia.Focused()
	// In xterm.js Ctrl+A is line start, Shift+arrows are not a selection, and Ctrl+C without a selection is SIGINT.
	// An explicit combo is trusted: the user can see what is selected.
	if focusedClass == xtermClass && !surelySelects(gesture) {
		log.Printf("[%s] %-12s skipped: xterm, selection not guaranteed", proc, gesture)
		return s, false
	}

	t0 := time.Now()
	text, how := "", ""
	uiaSupported := true
	if cfg.UseUIA() {
		if e.noUIA[proc] >= noUIALimit && cfg.UseCopy() {
			how = "uia: skipped for this app"
		} else {
			text, how, uiaSupported = e.uia.Selection(pt)
			if uiaSupported {
				delete(e.noUIA, proc)
			}
		}
	}
	if text == "" && cfg.UseCopy() {
		var note string
		text, note = e.copySelection(cfg, proc, focusedEdit)
		how = join(how, "copy: "+note)
		if text != "" && !uiaSupported {
			e.noUIA[proc]++
			if e.noUIA[proc] == noUIALimit {
				log.Printf("[%s] UIA doesn't work here; going straight to Ctrl+C", proc)
			}
		}
	}
	return selection{text, how, proc, fg, focusedEdit, time.Since(t0).Round(time.Millisecond)}, true
}

// copySelection gets the selection via Ctrl+C and restores the clipboard to its previous state.
func (e *Engine) copySelection(cfg *config.Config, proc string, focusedEdit bool) (string, string) {
	if con := win.ConsoleWindow(); con != 0 && con == win.ForegroundWindow() {
		return "", "skipped: own console"
	}
	if cfg.IsTerminal(proc) {
		return "", "skipped: terminal"
	}
	if cfg.CopyNeedsEdit(proc) && !focusedEdit {
		return "", "skipped: not an input field"
	}
	if m := win.HeldModifier(); m != "" {
		return "", "skipped: held" + m
	}

	saved, ok := e.saveClipboard(proc)
	if !ok {
		return "", "clipboard busy"
	}
	seq := clip.SequenceNumber()
	win.SendKeys(win.VKControl, win.VKC)

	changed := false
	for deadline := time.Now().Add(cfg.CopyTimeout()); time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		if clip.SequenceNumber() != seq {
			changed = true
			break
		}
	}
	if !changed {
		return "", "clipboard unchanged"
	}
	time.Sleep(10 * time.Millisecond) // some apps write several formats in a row

	text := clip.Text()
	clip.Set(saved.Items, true)
	if text != "" && cfg.CleanHistory && e.history != nil {
		e.history.Forget(text) // the entry from our Ctrl+C is useless in Win+V history
	}
	return text, "ok"
}

// saveClipboard is clip.Save that logs if saving was slow or incomplete.
func (e *Engine) saveClipboard(proc string) (clip.Snapshot, bool) {
	s, ok := clip.Save()
	if ok && (s.Took > slowSave || s.Skipped > 0) {
		log.Printf("[%s] clipboard save: %v, formats %d, oversized skipped %d",
			proc, s.Took.Round(time.Millisecond), len(s.Items), s.Skipped)
	}
	return s, ok
}

// How long to wait for the user to release the modifier after a middle-button combo.
const modifierReleaseWait = 3 * time.Second

func (e *Engine) onMiddle(pt win.Point) {
	t0 := time.Now()
	cfg := e.cfg.Load()
	proc := win.ProcessAt(pt)
	primary := e.Primary()
	mod := win.HeldModifier()

	// Modifier + middle button combos: save selection, menu, repeat last action.
	combo := ""
	if mod != "" && !cfg.IsDisabled(proc) {
		switch {
		case cfg.HotkeyCapture() && strings.EqualFold(mod, cfg.CaptureModifier):
			combo = gestureHotkey
		case e.menu != nil && strings.EqualFold(mod, cfg.MenuModifier):
			combo = gestureMenu
		case strings.EqualFold(mod, cfg.RepeatModifier):
			combo = gestureRepeat
		}
	}
	paste, why := false, "combo "+combo
	if combo != "" {
		if pass, reason := e.comboYields(cfg, pt, proc); pass {
			combo, why = "", reason
		}
	} else {
		paste, why = e.shouldPaste(cfg, pt, proc, primary, mod)
	}

	e.mid.Lock()
	up := e.mid.upSeen
	switch {
	case up:
		e.mid.state = midIdle
	case combo != "" || paste:
		e.mid.state = midSwallow
	default:
		e.mid.state = midForward
	}
	e.mid.Unlock()

	if combo == "" && !paste {
		if up {
			win.SendMouse(win.MouseMidDown, win.MouseMidUp)
		} else {
			win.SendMouse(win.MouseMidDown)
		}
		log.Printf("[%s] middle-click -> passed to the app (%s, %v)", proc, why, time.Since(t0).Round(time.Millisecond))
		return
	}

	// Our Ctrl+C / Ctrl+V would mix with the held modifier, so wait until it is released.
	// A dummy key stops releasing Alt/Win from opening the app menu or "Start".
	if mod != "" {
		win.MaskModifierRelease()
		if !waitModifiersReleased(modifierReleaseWait) {
			log.Printf("[%s] middle-click ✘ modifier not released within %v", proc, modifierReleaseWait)
			return
		}
	}
	switch combo {
	case gestureHotkey:
		e.onSelection(gestureHotkey, pt)
		return
	case gestureMenu:
		e.onMenu(cfg, pt)
		return
	case gestureRepeat:
		e.onRepeat(cfg, pt)
		return
	}

	note, ok := e.paste(cfg, primary, proc, true)
	if !ok {
		log.Printf("[%s] middle-click ✘ clipboard busy", proc)
		return
	}
	log.Printf("[%s] middle-click ✔ pasted %s (%s; %s, %v)", proc, describeText(cfg, primary), why, note, time.Since(t0).Round(time.Millisecond))
	e.showOSD(cfg, i18n.T("Pasted"), primary, true)
}

func waitModifiersReleased(limit time.Duration) bool {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if win.HeldModifier() == "" {
			return true
		}
	}
	return false
}

// comboYields reports whether to hand a modifier + middle-button combo to the app. In apps where the middle
// button has its own meaning (browsers, Explorer), Ctrl/Shift+middle open links in tabs,
// so over a link, or when there is definitely no selection, our combo doesn't get in the way.
func (e *Engine) comboYields(cfg *config.Config, pt win.Point, proc string) (bool, string) {
	if !cfg.PasteNeedsEdit(proc) {
		return false, ""
	}
	if e.uia.HyperlinkAt(pt) {
		return true, "combo over a link"
	}
	if text, _, supported := e.uia.Selection(pt); supported && strings.TrimSpace(text) == "" {
		return true, "combo without a selection"
	}
	return false, ""
}

// shouldPaste decides whether to paste; mod is the held modifier ("" for none).
func (e *Engine) shouldPaste(cfg *config.Config, pt win.Point, proc, primary, mod string) (bool, string) {
	if primary == "" {
		return false, "primary is empty"
	}
	if cfg.IsDisabled(proc) {
		return false, "disabled for this app"
	}
	// Other middle-button combos have their own meaning (Ctrl opens a background tab, etc.).
	if !strings.EqualFold(mod, cfg.PasteModifier) {
		if mod == "" {
			return false, "paste only with " + cfg.PasteModifier
		}
		return false, "held " + mod
	}
	if cfg.PasteNeedsEdit(proc) {
		if e.uia.EditAt(pt) {
			return true, "input field"
		}
		return false, "not an input field: " + e.uia.DescribeAt(pt)
	}
	// Before pasting we left-click; over a button or menu item that would press it.
	if ct, ok := e.uia.ClickableAt(pt); ok {
		return false, fmt.Sprintf("control under cursor type=%d", ct)
	}
	return true, "regular app"
}

// After the app has read the text we wait a bit longer: some re-read the clipboard or ask for other formats.
const afterRenderMargin = 30 * time.Millisecond

// paste pastes text without leaving it on the clipboard; click first places the caret under the cursor
// (without it the paste replaces the current selection). note describes how the paste went (for the log);
// if someone else changed the clipboard while waiting, their data is not overwritten.
func (e *Engine) paste(cfg *config.Config, text, proc string, click bool) (note string, ok bool) {
	if click {
		win.SendMouse(win.MouseLeftDown, win.MouseLeftUp)
		time.Sleep(40 * time.Millisecond)
	}

	saved, ok := e.saveClipboard(proc)
	if !ok {
		return "", false
	}

	var rendered <-chan struct{}
	if e.lazy != nil {
		rendered, ok = e.lazy.Offer(text)
	} else {
		ok = clip.Set([]clip.Item{clip.TextItem(text)}, true)
	}
	if !ok {
		return "", false
	}
	ours := clip.SequenceNumber()

	t0 := time.Now()
	if cfg.IsTerminal(proc) {
		win.SendKeys(win.VKShift, win.VKInsert)
	} else {
		win.SendKeys(win.VKControl, win.VKV)
	}

	if rendered == nil {
		time.Sleep(cfg.RestoreDelay()) // without delayed rendering just wait
		note = "fixed delay"
	} else {
		select {
		case <-rendered:
			note = fmt.Sprintf("read in %v", time.Since(t0).Round(time.Millisecond))
			time.Sleep(afterRenderMargin)
		case <-time.After(cfg.PasteTimeout()):
			note = fmt.Sprintf("app did not read the clipboard within %v", cfg.PasteTimeout())
		}
	}

	if cur := clip.SequenceNumber(); cur != ours && (e.lazy == nil || cur != e.lazy.RenderSeq()) {
		return note + "; clipboard changed by someone else; not restoring", true
	}
	clip.Set(saved.Items, true)
	return note, true
}

func describeText(cfg *config.Config, s string) string {
	n := len([]rune(s))
	if !cfg.LogText {
		return fmt.Sprintf("%d characters", n)
	}
	if n > 80 {
		return fmt.Sprintf("%q… (%d characters)", string([]rune(s)[:80]), n)
	}
	return fmt.Sprintf("%q", s)
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + " → " + b
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
