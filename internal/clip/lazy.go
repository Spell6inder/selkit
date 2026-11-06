package clip

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	procRegisterClassEx   = user32.NewProc("RegisterClassExW")
	procCreateWindowEx    = user32.NewProc("CreateWindowExW")
	procDefWindowProc     = user32.NewProc("DefWindowProcW")
	procGetMessage        = user32.NewProc("GetMessageW")
	procDispatchMessage   = user32.NewProc("DispatchMessageW")
	procGetClipboardOwner = user32.NewProc("GetClipboardOwner")
	procGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmRenderFormat     = 0x0305
	wmRenderAllFormats = 0x0306
	hwndMessage        = ^uintptr(2) // HWND_MESSAGE = (HWND)-3
)

// Lazy serves text through delayed rendering: only a CF_UNICODETEXT "promise" is placed on the clipboard,
// and the real data is supplied when an application asks for it (WM_RENDERFORMAT). This shows the exact moment
// the paste read the clipboard, so it can be restored immediately instead of after a fixed pause.
type Lazy struct {
	hwnd uintptr

	mu        sync.Mutex
	text      string
	rendered  chan struct{}
	renderSeq uintptr
}

// StartLazy creates a hidden message-only window (the clipboard owner) in its own thread with a message loop.
func StartLazy() (*Lazy, error) {
	l := &Lazy{}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		hwnd, err := createMessageWindow("selkit-clipboard", l.wndProc)
		if err != nil {
			ready <- err
			return
		}
		l.hwnd = hwnd
		ready <- nil
		var m struct {
			Hwnd    uintptr
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Pt      struct{ X, Y int32 }
			Private uint32
		}
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return l, nil
}

// Offer places a promise of text on the clipboard (with "not for history" markers).
// The channel is closed as soon as some application reads the text.
func (l *Lazy) Offer(text string) (<-chan struct{}, bool) {
	ch := make(chan struct{})
	l.mu.Lock()
	l.text, l.rendered, l.renderSeq = text, ch, 0
	l.mu.Unlock()

	if !openAs(l.hwnd) {
		return nil, false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	procSetClipboardData.Call(cfUnicodeText, 0)
	setMarkers()
	return ch, true
}

// RenderSeq is the clipboard sequence number right after we supplied the data.
func (l *Lazy) RenderSeq() uintptr {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.renderSeq
}

func (l *Lazy) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmRenderFormat: // the clipboard is already open by the reading application; only SetClipboardData
		if wParam == cfUnicodeText {
			l.render()
		}
		return 0
	case wmRenderAllFormats: // we are exiting while the promise is still on the clipboard
		if openAs(hwnd) {
			if owner, _, _ := procGetClipboardOwner.Call(); owner == hwnd {
				l.render()
			}
			procCloseClipboard.Call()
		}
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

func (l *Lazy) render() {
	l.mu.Lock()
	text, ch := l.text, l.rendered
	l.rendered = nil
	l.mu.Unlock()

	setData(TextItem(text))

	l.mu.Lock()
	l.renderSeq = SequenceNumber()
	l.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

func createMessageWindow(class string, proc func(hwnd, msg, wParam, lParam uintptr) uintptr) (uintptr, error) {
	hInst, _, _ := procGetModuleHandle.Call(0)
	name, _ := syscall.UTF16PtrFromString(class)
	wc := wndClassEx{
		WndProc:   syscall.NewCallback(proc),
		Instance:  hInst,
		ClassName: name,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return 0, fmt.Errorf("RegisterClassEx: %w", err)
	}
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0, hwndMessage, 0, hInst, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowEx: %w", err)
	}
	return hwnd, nil
}
