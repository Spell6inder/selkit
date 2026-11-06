package win

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Rect struct{ Left, Top, Right, Bottom int32 }

func WindowAt(pt Point) uintptr {
	hwnd, _, _ := procWindowFromPoint.Call(pt.Packed())
	return hwnd
}

// RootWindowAt returns the top-level window under the point (to compare its position before and after a drag).
func RootWindowAt(pt Point) uintptr {
	r, _, _ := procGetAncestor.Call(WindowAt(pt), 2) // GA_ROOT
	return r
}

func WindowRect(hwnd uintptr) Rect {
	var r Rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func WindowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

var (
	selfElevated = sync.OnceValue(func() bool { return windows.GetCurrentProcessToken().IsElevated() })

	elevMu    sync.Mutex
	elevCache = map[uint32]bool{}
)

// ForeignElevated reports whether the window belongs to a process with higher privileges than ours.
// UIPI blocks SendInput and UIA there, so such windows should be left alone.
// Fast enough to call from the hook: the result is cached by PID.
func ForeignElevated(hwnd uintptr) bool {
	if selfElevated() {
		return false
	}
	pid := WindowPID(hwnd)
	if pid == 0 {
		return false
	}
	elevMu.Lock()
	defer elevMu.Unlock()
	if v, ok := elevCache[pid]; ok {
		return v
	}
	if len(elevCache) > 512 { // PIDs are reused, so do not keep the cache forever
		clear(elevCache)
	}
	v := processElevated(pid)
	elevCache[pid] = v
	return v
}

func processElevated(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		// No rights even to read the token: almost certainly a higher-integrity process.
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer tok.Close()
	return tok.IsElevated()
}

var (
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
)

// ForceForeground makes hwnd the active window. Windows allows this only for the process the user
// has just interacted with, so for the duration of the call we attach our thread to the input queue of the current active window.
func ForceForeground(hwnd uintptr) bool {
	fg := ForegroundWindow()
	if fg == hwnd {
		return true
	}
	fgThread, _, _ := procGetWindowThreadProcessId.Call(fg, 0)
	self, _, _ := procGetCurrentThreadId.Call()
	attached := false
	if fgThread != 0 && fgThread != self {
		r, _, _ := procAttachThreadInput.Call(self, fgThread, 1)
		attached = r != 0
	}
	r, _, _ := procSetForegroundWindow.Call(hwnd)
	if attached {
		procAttachThreadInput.Call(self, fgThread, 0)
	}
	return r != 0
}
