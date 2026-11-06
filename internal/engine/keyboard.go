package engine

import "github.com/Spell6inder/selkit/internal/win"

// Keyboard selection events in the shared channel (they don't overlap with the mouse WM_* codes).
const (
	evKeySelect = 1 // Shift+arrows/Home/End/PgUp/PgDn
	evSelectAll = 2 // Ctrl+A
)

var navKeys = map[uint32]bool{
	win.VKLeft: true, win.VKRight: true, win.VKUp: true, win.VKDown: true,
	win.VKHome: true, win.VKEnd: true, win.VKPrior: true, win.VKNext: true,
}

var modifierKeys = map[uint32]bool{
	win.VKShift: true, win.VKControl: true, win.VKMenu: true,
	win.VKLShift: true, win.VKRShift: true, win.VKLControl: true, win.VKRControl: true,
	win.VKLMenu: true, win.VKRMenu: true, win.VKLWin: true, win.VKRWin: true,
}

// kbState is for the hook thread only.
type kbState struct {
	down    map[uint32]bool // held modifiers
	pending uintptr         // evKeySelect / evSelectAll that fires once all modifiers are released
}

// keyboardProc tracks keyboard selection. It fires when the last modifier is released:
// earlier, Ctrl+C would mix with the user's held keys.
// Any other key between the gesture and the release (Ctrl+A -> V, Shift+Left -> Delete) cancels it:
// the selection has already been replaced or deleted.
func (e *Engine) keyboardProc(nCode, wParam, lParam uintptr) uintptr {
	if cfg := e.cfg.Load(); int32(nCode) >= 0 && e.enabled.Load() && cfg.KeyboardSelection && !cfg.HotkeyCapture() {
		if k := win.ParseKeyboardHook(lParam); !k.Injected {
			e.onKey(k)
		}
	}
	return win.CallNextHook(nCode, wParam, lParam)
}

func (e *Engine) onKey(k win.KeyHookEvent) {
	s := &e.kb
	if s.down == nil {
		s.down = map[uint32]bool{}
	}

	if modifierKeys[k.VK] {
		if !k.Up {
			s.down[k.VK] = true
			return
		}
		delete(s.down, k.VK)
		// A missed keyup (Win+L, desktop switch) would leave the modifier "held" forever,
		// so check against the real state. For keys other than the current one it is already up to date.
		for vk := range s.down {
			if !win.KeyDown(int(vk)) {
				delete(s.down, vk)
			}
		}
		if len(s.down) == 0 && s.pending != 0 {
			e.post(mouseEvent{msg: s.pending, pt: win.CursorPos()})
			s.pending = 0
		}
		return
	}
	if k.Up {
		return
	}

	shift := s.down[win.VKLShift] || s.down[win.VKRShift] || s.down[win.VKShift]
	ctrl := s.down[win.VKLControl] || s.down[win.VKRControl] || s.down[win.VKControl]
	other := s.down[win.VKLMenu] || s.down[win.VKRMenu] || s.down[win.VKMenu] || s.down[win.VKLWin] || s.down[win.VKRWin]
	switch {
	case navKeys[k.VK] && shift && !other:
		s.pending = evKeySelect
	case k.VK == win.VKA && ctrl && !shift && !other:
		s.pending = evSelectAll
	default:
		s.pending = 0
	}
}
