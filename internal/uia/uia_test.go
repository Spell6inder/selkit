package uia

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procPeekMessage     = user32.NewProc("PeekMessageW")
	procDispatchMessage = user32.NewProc("DispatchMessageW")
)

// Verifies the vtable indices missing from the documentation at hand (BoundingRectangle, ValuePattern.IsReadOnly)
// on real Win32 controls. Briefly shows a few fields in a screen corner, so it runs only when requested explicitly:
// SELKIT_UI_TEST=1 go test ./internal/uia
func TestWin32Controls(t *testing.T) {
	if os.Getenv("SELKIT_UI_TEST") == "" {
		t.Skip("SELKIT_UI_TEST not set")
	}

	// The windows live in a separate thread with a message loop; UIA reaches them through WM_GETOBJECT.
	type ctl struct {
		class string
		style uintptr
		hwnd  uintptr
	}
	ctls := []*ctl{
		{class: "EDIT"},
		{class: "EDIT", style: 0x0800},     // ES_READONLY
		{class: "COMBOBOX", style: 0x0002}, // CBS_DROPDOWN: with an edit field
		{class: "COMBOBOX", style: 0x0003}, // CBS_DROPDOWNLIST: selection only
	}
	created, done := make(chan struct{}), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		for i, c := range ctls {
			cls, _ := syscall.UTF16PtrFromString(c.class)
			c.hwnd, _, _ = procCreateWindowEx.Call(0x00000008, uintptr(unsafe.Pointer(cls)), 0, // WS_EX_TOPMOST
				0x80000000|0x10000000|c.style, // WS_POPUP | WS_VISIBLE
				uintptr(20+i*210), 20, 200, 24, 0, 0, 0, 0)
		}
		close(created)
		var m [48]byte
		for {
			select {
			case <-done:
				for _, c := range ctls {
					procDestroyWindow.Call(c.hwnd)
				}
				return
			default:
			}
			for {
				if r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0, 1); r == 0 {
					break
				}
				procDispatchMessage.Call(uintptr(unsafe.Pointer(&m[0])))
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	<-created
	defer close(done)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	InitThread()
	c, _, err := New(time.Second)
	if err != nil {
		t.Fatal(err)
	}

	element := func(hwnd uintptr) uintptr {
		var el uintptr
		if !hrOK(comCall(c.p, vtElementFromHandle, hwnd, uintptr(unsafe.Pointer(&el)))) || el == 0 {
			t.Fatalf("ElementFromHandle(%x) failed", hwnd)
		}
		return el
	}

	for i, ctl := range ctls {
		if ctl.hwnd == 0 {
			t.Fatalf("failed to create %s", ctl.class)
		}
		el := element(ctl.hwnd)
		var want rect
		procGetWindowRect.Call(ctl.hwnd, uintptr(unsafe.Pointer(&want)))
		if got := boundingRect(el); got != want {
			t.Errorf("#%d %s: BoundingRectangle = %+v, GetWindowRect = %+v", i, ctl.class, got, want)
		}
		ro, ok := valueReadOnly(el)
		t.Logf("#%d %s style=%#x: type=%d ValuePattern ok=%v readOnly=%v isEditable=%v",
			i, ctl.class, ctl.style, controlType(el), ok, ro, isEditable(el))
		release(el)
	}

	// A plain field is editable, ES_READONLY is not: this is how we check the IsReadOnly index.
	el := element(ctls[0].hwnd)
	if ro, ok := valueReadOnly(el); !ok || ro {
		t.Errorf("EDIT: ValuePattern ok=%v readOnly=%v, want ok=true readOnly=false", ok, ro)
	}
	release(el)
	el = element(ctls[1].hwnd)
	if ro, ok := valueReadOnly(el); !ok || !ro {
		t.Errorf("EDIT ES_READONLY: ValuePattern ok=%v readOnly=%v, want ok=true readOnly=true", ok, ro)
	}
	release(el)
}
