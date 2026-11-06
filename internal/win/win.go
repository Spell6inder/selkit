// Package win provides thin wrappers over WinAPI: mouse hook, SendInput, processes, small UI helpers.
package win

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procSetWindowsHookEx         = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx      = user32.NewProc("UnhookWindowsHookEx")
	procSetTimer                 = user32.NewProc("SetTimer")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetAncestor              = user32.NewProc("GetAncestor")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procCallNextHookEx           = user32.NewProc("CallNextHookEx")
	procGetMessage               = user32.NewProc("GetMessageW")
	procGetDoubleClickTime       = user32.NewProc("GetDoubleClickTime")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procWindowFromPoint          = user32.NewProc("WindowFromPoint")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procSendInput                = user32.NewProc("SendInput")
	procMessageBox               = user32.NewProc("MessageBoxW")
	procSetDpiAwarenessContext   = user32.NewProc("SetProcessDpiAwarenessContext")

	procGetModuleHandle           = kernel32.NewProc("GetModuleHandleW")
	procOpenProcess               = kernel32.NewProc("OpenProcess")
	procCloseHandle               = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetConsoleWindow          = kernel32.NewProc("GetConsoleWindow")
	procCreateMutex               = kernel32.NewProc("CreateMutexW")

	procShellExecute = shell32.NewProc("ShellExecuteW")
)

const (
	WMLButtonDown = 0x0201
	WMLButtonUp   = 0x0202
	WMMButtonDown = 0x0207
	WMMButtonUp   = 0x0208

	SMCxDoubleClk = 36
	SMCyDoubleClk = 37
	SMCxDrag      = 68
	SMCyDrag      = 69

	VKShift   = 0x10
	VKControl = 0x11
	VKMenu    = 0x12
	VKInsert  = 0x2D
	VKLWin    = 0x5B
	VKRWin    = 0x5C
	vkMask    = 0xE8 // not assigned to any key
	VKA       = 0x41
	VKC       = 0x43
	VKV       = 0x56

	VKPrior    = 0x21 // PageUp
	VKNext     = 0x22 // PageDown
	VKEnd      = 0x23
	VKHome     = 0x24
	VKLeft     = 0x25
	VKUp       = 0x26
	VKRight    = 0x27
	VKDown     = 0x28
	VKLShift   = 0xA0
	VKRShift   = 0xA1
	VKLControl = 0xA2
	VKRControl = 0xA3
	VKLMenu    = 0xA4
	VKRMenu    = 0xA5

	MouseLeftDown = 0x0002
	MouseLeftUp   = 0x0004
	MouseMidDown  = 0x0020
	MouseMidUp    = 0x0040

	whMouseLL         = 14
	whKeyboardLL      = 13
	wmTimer           = 0x0113
	llmhfInjected     = 0x01
	llkhfInjected     = 0x10
	llkhfUp           = 0x80
	inputMouse        = 0
	inputKeyboard     = 1
	keyEventfExtended = 0x0001
	keyEventfKeyUp    = 0x0002
	processQueryLtd   = 0x1000
	errAlreadyExists  = 183
)

type Point struct{ X, Y int32 }

// Packed is a POINT passed by value (on amd64 it fits in a single 64-bit register).
func (p Point) Packed() uintptr { return uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32 }

// MouseHookEvent is a parsed MSLLHOOKSTRUCT.
type MouseHookEvent struct {
	Pt       Point
	Time     uint32
	Injected bool
}

type msllHookStruct struct {
	Pt        Point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// ParseMouseHook reads the lParam of a WH_MOUSE_LL callback.
func ParseMouseHook(lParam uintptr) MouseHookEvent {
	h := (*msllHookStruct)(unsafe.Pointer(lParam))
	return MouseHookEvent{h.Pt, h.Time, h.Flags&llmhfInjected != 0}
}

// KeyHookEvent is a parsed KBDLLHOOKSTRUCT.
type KeyHookEvent struct {
	VK       uint32
	Up       bool
	Injected bool
}

type kbdllHookStruct struct {
	VKCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// ParseKeyboardHook reads the lParam of a WH_KEYBOARD_LL callback.
func ParseKeyboardHook(lParam uintptr) KeyHookEvent {
	h := (*kbdllHookStruct)(unsafe.Pointer(lParam))
	return KeyHookEvent{h.VKCode, h.Flags&llkhfUp != 0, h.Flags&llkhfInjected != 0}
}

type HookProc func(nCode, wParam, lParam uintptr) uintptr

// Callbacks are created once (the number of NewCallback slots is limited); the handles are kept for reinstallation.
var hooks []struct{ id, callback, handle uintptr }

// SetHooks installs WH_MOUSE_LL and (if keyboard != nil) WH_KEYBOARD_LL on the current thread.
// The thread must be locked (runtime.LockOSThread) and run RunMessageLoop.
func SetHooks(mouse, keyboard HookProc) error {
	hooks = append(hooks[:0], struct{ id, callback, handle uintptr }{whMouseLL, syscall.NewCallback(mouse), 0})
	if keyboard != nil {
		hooks = append(hooks, struct{ id, callback, handle uintptr }{whKeyboardLL, syscall.NewCallback(keyboard), 0})
	}
	return installHooks()
}

// ReinstallHooks removes and reinstalls the hooks in case Windows silently dropped them
// after LowLevelHooksTimeout was exceeded. Call it from the hook thread.
func ReinstallHooks() error {
	for i := range hooks {
		if hooks[i].handle != 0 {
			procUnhookWindowsHookEx.Call(hooks[i].handle)
			hooks[i].handle = 0
		}
	}
	return installHooks()
}

func installHooks() error {
	hMod, _, _ := procGetModuleHandle.Call(0)
	for i := range hooks {
		h, _, err := procSetWindowsHookEx.Call(hooks[i].id, hooks[i].callback, hMod, 0)
		if h == 0 {
			return fmt.Errorf("SetWindowsHookEx(%d): %w", hooks[i].id, err)
		}
		hooks[i].handle = h
	}
	return nil
}

func CallNextHook(nCode, wParam, lParam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

// StartThreadTimer posts WM_TIMER to the current thread's queue every interval ms.
func StartThreadTimer(intervalMs uint32) {
	procSetTimer.Call(0, 0, uintptr(intervalMs), 0)
}

func CursorPos() Point {
	var p Point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
	Private uint32
}

// RunMessageLoop runs GetMessage on the current (locked) thread; without it the hook receives no events.
// onTimer is called for every WM_TIMER from StartThreadTimer.
func RunMessageLoop(onTimer func()) {
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if m.Message == wmTimer && m.Hwnd == 0 && onTimer != nil {
			onTimer()
		}
	}
}

// INPUT with KEYBDINPUT / MOUSEINPUT: both are 40 bytes on amd64 (the union is aligned to MOUSEINPUT).
type keyInput struct {
	Type  uint32
	_     uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	_     uint32
	Extra uintptr
	_     [8]byte
}

type mouseInput struct {
	Type      uint32
	_         uint32
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	Extra     uintptr
}

// SendKeys presses the keys in order and releases them in reverse order.
func SendKeys(vks ...uint16) {
	var in []keyInput
	for _, vk := range vks {
		in = append(in, keyInput{Type: inputKeyboard, Vk: vk, Flags: extFlag(vk)})
	}
	for i := len(vks) - 1; i >= 0; i-- {
		in = append(in, keyInput{Type: inputKeyboard, Vk: vks[i], Flags: extFlag(vks[i]) | keyEventfKeyUp})
	}
	procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

func extFlag(vk uint16) uint32 {
	if vk == VKInsert {
		return keyEventfExtended
	}
	return 0
}

// SendMouse sends button events at the current cursor position.
func SendMouse(flags ...uint32) {
	in := make([]mouseInput, len(flags))
	for i, f := range flags {
		in[i] = mouseInput{Type: inputMouse, Flags: f}
	}
	procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

func ForegroundWindow() uintptr {
	fg, _, _ := procGetForegroundWindow.Call()
	return fg
}

func ConsoleWindow() uintptr {
	h, _, _ := procGetConsoleWindow.Call()
	return h
}

func ForegroundProcess() string { return ProcessOf(ForegroundWindow()) }

func ProcessAt(pt Point) string {
	hwnd, _, _ := procWindowFromPoint.Call(pt.Packed())
	return ProcessOf(hwnd)
}

// ProcessOf returns the exe file name of the process that owns the window.
func ProcessOf(hwnd uintptr) string {
	pid := WindowPID(hwnd)
	h, _, _ := procOpenProcess.Call(processQueryLtd, 0, uintptr(pid))
	if h == 0 {
		return fmt.Sprintf("pid %d", pid)
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return fmt.Sprintf("pid %d", pid)
	}
	return filepath.Base(syscall.UTF16ToString(buf[:n]))
}

func DoubleClickTime() uint32 {
	r, _, _ := procGetDoubleClickTime.Call()
	return uint32(r)
}

func Metric(i int) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(i))
	return int32(r)
}

func KeyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// HeldModifier returns the name of the physically held modifier, or "".
// Our SendInput combinations would mix with it, and releasing Ctrl would break the user's key state.
func HeldModifier() string {
	switch {
	case KeyDown(VKControl):
		return "Ctrl"
	case KeyDown(VKShift):
		return "Shift"
	case KeyDown(VKMenu):
		return "Alt"
	case KeyDown(VKLWin), KeyDown(VKRWin):
		return "Win"
	}
	return ""
}

// MaskModifierRelease presses an unused key (VK 0xE8) while the user holds a modifier,
// so that releasing a lone Alt does not activate the app menu and Win does not open "Start".
func MaskModifierRelease() { SendKeys(vkMask) }

// SetDPIAware enables per-monitor DPI awareness v2. The mouse hook reports physical coordinates, and without it
// WindowFromPoint/ElementFromPoint on scaled monitors would treat them as logical.
// Call before creating any windows.
func SetDPIAware() error {
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4
	if r, _, err := procSetDpiAwarenessContext.Call(perMonitorAwareV2); r == 0 {
		if err == syscall.ERROR_ACCESS_DENIED {
			return nil // already set by the exe manifest
		}
		return fmt.Errorf("SetProcessDpiAwarenessContext: %w", err)
	}
	return nil
}

// singleInstanceMutex is held until the process exits.
var singleInstanceMutex uintptr

// SingleInstance returns false if another process has already created the named mutex.
func SingleInstance(name string) bool {
	h, _, err := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(utf16Ptr(name))))
	if h == 0 {
		return true // could not check, so do not block startup
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errAlreadyExists {
		procCloseHandle.Call(h)
		return false
	}
	singleInstanceMutex = h
	return true
}

func MessageBox(title, text string) {
	procMessageBox.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), 0x40) // MB_ICONINFORMATION
}

// ShellOpen opens a file with its default program.
func ShellOpen(path string) error {
	r, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(path))), 0, 0, 1) // SW_SHOWNORMAL
	if r <= 32 {
		return fmt.Errorf("ShellExecute(%s): code %d", path, r)
	}
	return nil
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
