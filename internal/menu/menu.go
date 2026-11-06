// Package menu is a standard Windows context menu near the cursor.
//
// The menu needs its own window and a thread with a message loop: TrackPopupMenuEx is modal and pumps
// messages itself, and closes on a click outside the menu only when our window is active.
// So we take focus for the duration of the menu; the caller of Show must return it to the previous window.
package menu

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/Spell6inder/selkit/internal/win"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procGetMessage       = user32.NewProc("GetMessageW")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenu       = user32.NewProc("AppendMenuW")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procTrackPopupMenuEx = user32.NewProc("TrackPopupMenuEx")
	procGetModuleHandle  = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
)

const (
	wsPopup        = 0x80000000
	wsExToolWindow = 0x00000080
	wsExTopmost    = 0x00000008

	wmNull = 0x0000
	wmShow = 0x8000 + 1 // WM_APP+1

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080
	tpmRightButton = 0x0002
)

// Item is a menu item. An empty Label is a separator; Sub is a submenu; ID 0 is a disabled item.
// Hint is shown on the right (like a key hint); the result preview goes there. Checked puts a check mark
// in the menu's own column, so the label does not move.
type Item struct {
	Label, Hint string
	ID          int
	Checked     bool
	Sub         []Item
}

type result struct {
	id     int
	active bool
}

type request struct {
	items []Item
	at    win.Point
	res   chan result
}

type Menu struct {
	hwnd uintptr
	reqs chan request
}

func Start() (*Menu, error) {
	m := &Menu{reqs: make(chan request, 1)}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := m.create(); err != nil {
			ready <- err
			return
		}
		ready <- nil
		var msg [48]byte
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return m, nil
}

// Show displays the menu at point at and blocks until the user picks an item; 0 means the menu was dismissed.
// active reports whether focus was taken (without it the menu does not close on an outside click).
func (m *Menu) Show(items []Item, at win.Point) (id int, active bool) {
	req := request{items, at, make(chan result, 1)}
	m.reqs <- req
	procPostMessage.Call(m.hwnd, wmShow, 0, 0)
	r := <-req.res
	return r.id, r.active
}

func (m *Menu) create() error {
	hInst, _, _ := procGetModuleHandle.Call(0)
	name, _ := syscall.UTF16PtrFromString("selkit-menu")
	wc := struct {
		Size, Style                uint32
		WndProc                    uintptr
		ClsExtra, WndExtra         int32
		Instance, Icon, Cursor, Bg uintptr
		MenuName, ClassName        *uint16
		IconSm                     uintptr
	}{WndProc: syscall.NewCallback(m.wndProc), Instance: hInst, ClassName: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	// Invisible top-level window (not message-only: that cannot become active).
	hwnd, _, err := procCreateWindowEx.Call(wsExToolWindow|wsExTopmost,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), wsPopup,
		0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	m.hwnd = hwnd
	return nil
}

func (m *Menu) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmShow {
		select {
		case req := <-m.reqs:
			m.track(req)
		default:
		}
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

func (m *Menu) track(req request) {
	var keep [][]uint16 // item strings must stay alive while the menu is open
	h := build(req.items, &keep)
	defer procDestroyMenu.Call(h)
	active := win.ForceForeground(m.hwnd)
	id, _, _ := procTrackPopupMenuEx.Call(h, tpmReturnCmd|tpmNoNotify|tpmRightButton,
		uintptr(req.at.X), uintptr(req.at.Y), m.hwnd, 0)
	// MSDN recommendation: without this a repeated menu sometimes closes immediately.
	procPostMessage.Call(m.hwnd, wmNull, 0, 0)
	runtime.KeepAlive(keep)
	req.res <- result{int(id), active}
}

func build(items []Item, keep *[][]uint16) uintptr {
	h, _, _ := procCreatePopupMenu.Call()
	for _, it := range items {
		switch {
		case it.Label == "":
			procAppendMenu.Call(h, mfSeparator, 0, 0)
		case it.Sub != nil:
			procAppendMenu.Call(h, mfPopup, build(it.Sub, keep), str(keep, it.Label))
		default:
			flags := uintptr(mfString)
			if it.ID == 0 {
				flags |= mfGrayed
			}
			if it.Checked {
				flags |= mfChecked
			}
			label := it.Label
			if it.Hint != "" {
				label += "\t" + it.Hint
			}
			procAppendMenu.Call(h, flags, uintptr(it.ID), str(keep, label))
		}
	}
	return h
}

func str(keep *[][]uint16, s string) uintptr {
	u, _ := syscall.UTF16FromString(s)
	*keep = append(*keep, u)
	return uintptr(unsafe.Pointer(&u[0]))
}
