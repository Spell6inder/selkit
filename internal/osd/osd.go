// Package osd is a short tooltip near the cursor ("what is in primary now").
//
// It is a transparent topmost window: takes no focus, passes clicks through and disappears on its own.
// It lives in its own thread with a message loop; Show may be called from any thread.
package osd

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Spell6inder/selkit/internal/win"
)

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	gdi32   = syscall.NewLazyDLL("gdi32.dll")
	shcore  = syscall.NewLazyDLL("shcore.dll")
	dwmapi  = syscall.NewLazyDLL("dwmapi.dll")
	kernel3 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx           = user32.NewProc("RegisterClassExW")
	procCreateWindowEx            = user32.NewProc("CreateWindowExW")
	procDefWindowProc             = user32.NewProc("DefWindowProcW")
	procGetMessage                = user32.NewProc("GetMessageW")
	procDispatchMessage           = user32.NewProc("DispatchMessageW")
	procPostMessage               = user32.NewProc("PostMessageW")
	procSetWindowPos              = user32.NewProc("SetWindowPos")
	procShowWindow                = user32.NewProc("ShowWindow")
	procSetTimer                  = user32.NewProc("SetTimer")
	procKillTimer                 = user32.NewProc("KillTimer")
	procInvalidateRect            = user32.NewProc("InvalidateRect")
	procBeginPaint                = user32.NewProc("BeginPaint")
	procEndPaint                  = user32.NewProc("EndPaint")
	procFillRect                  = user32.NewProc("FillRect")
	procDrawText                  = user32.NewProc("DrawTextW")
	procGetDC                     = user32.NewProc("GetDC")
	procReleaseDC                 = user32.NewProc("ReleaseDC")
	procSetLayeredWindowAttribute = user32.NewProc("SetLayeredWindowAttributes")
	procMonitorFromPoint          = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfo            = user32.NewProc("GetMonitorInfoW")
	procGetModuleHandle           = kernel3.NewProc("GetModuleHandleW")

	procCreateFont       = gdi32.NewProc("CreateFontW")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")

	procGetDpiForMonitor      = shcore.NewProc("GetDpiForMonitor")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wsPopup         = 0x80000000
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExTransparent = 0x00000020
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000

	wmPaint         = 0x000F
	wmTimer         = 0x0113
	wmNcHitTest     = 0x0084
	wmMouseActivate = 0x0021
	wmShow          = 0x8000 + 1 // WM_APP+1

	htTransparent = ^uintptr(0) // -1
	maNoActivate  = 3

	swHide           = 0
	swShowNoActivate = 4
	hwndTopmost      = ^uintptr(0) // (HWND)-1
	swpNoActivate    = 0x0010
	lwaAlpha         = 0x2

	dtWordBreak   = 0x0010
	dtSingleLine  = 0x0020
	dtExpandTabs  = 0x0040
	dtNoPrefix    = 0x0800
	dtCalcRect    = 0x0400
	dtEditControl = 0x2000
	dtEndEllipsis = 0x8000

	monitorDefaultToNearest = 2
	dwmwaCornerPreference   = 33
	dwmwcpRound             = 2

	timerHide = 1
	timerFade = 2
)

// Look: a dark plate, like Windows 11 system tooltips.
const (
	alphaFull   = 235
	fadeStep    = 30
	fadeEvery   = 16         // ms
	bgColor     = 0x00202020 // COLORREF: 0x00BBGGRR
	titleColor  = 0x00A8A8A8
	textColor   = 0x00F2F2F2
	maxWidthPt  = 340 // maximum text width in points (≈ 453 px at 96 DPI)
	maxBodyRows = 6   // more rows after wrapping are truncated
	paddingPt   = 8
	offsetPt    = 14 // offset from the cursor so the tooltip does not end up under it
)

type rect struct{ Left, Top, Right, Bottom int32 }

type request struct {
	title, body string
	at          win.Point
	duration    time.Duration
}

type OSD struct {
	hwnd uintptr

	mu  sync.Mutex
	req request

	// The rest is accessed only from the window thread.
	cur       request
	dpi       uint32
	titleFont uintptr
	bodyFont  uintptr
	bgBrush   uintptr
	titleRect rect
	bodyRect  rect
	alpha     int
}

// Start creates the hidden tooltip window in its own thread.
func Start() (*OSD, error) {
	o := &OSD{}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := o.create(); err != nil {
			ready <- err
			return
		}
		ready <- nil
		var m [48]byte // MSG
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&m[0])))
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return o, nil
}

// Show displays the tooltip near point at (usually the cursor) for duration.
// A new tooltip immediately replaces the previous one.
func (o *OSD) Show(title, body string, at win.Point, duration time.Duration) {
	o.mu.Lock()
	o.req = request{title, body, at, duration}
	o.mu.Unlock()
	procPostMessage.Call(o.hwnd, wmShow, 0, 0)
}

func (o *OSD) create() error {
	hInst, _, _ := procGetModuleHandle.Call(0)
	name, _ := syscall.UTF16PtrFromString("selkit-osd")
	wc := struct {
		Size, Style                uint32
		WndProc                    uintptr
		ClsExtra, WndExtra         int32
		Instance, Icon, Cursor, Bg uintptr
		MenuName, ClassName        *uint16
		IconSm                     uintptr
	}{WndProc: syscall.NewCallback(o.wndProc), Instance: hInst, ClassName: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	hwnd, _, err := procCreateWindowEx.Call(
		wsExTopmost|wsExToolWindow|wsExTransparent|wsExLayered|wsExNoActivate,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), wsPopup,
		0, 0, 1, 1, 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	o.hwnd = hwnd
	o.bgBrush, _, _ = procCreateSolidBrush.Call(bgColor)
	corner := uint32(dwmwcpRound) // rounded corners on Windows 11; on older systems the call simply has no effect
	procDwmSetWindowAttribute.Call(hwnd, dwmwaCornerPreference, uintptr(unsafe.Pointer(&corner)), 4)
	return nil
}

func (o *OSD) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmShow:
		o.mu.Lock()
		o.cur = o.req
		o.mu.Unlock()
		o.show()
		return 0
	case wmTimer:
		o.onTimer(wParam)
		return 0
	case wmPaint:
		o.paint()
		return 0
	case wmNcHitTest:
		return htTransparent
	case wmMouseActivate:
		return maNoActivate
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

func (o *OSD) show() {
	procKillTimer.Call(o.hwnd, timerFade)
	mon, _, _ := procMonitorFromPoint.Call(o.cur.at.Packed(), monitorDefaultToNearest)
	o.useDPI(monitorDPI(mon))

	w, h := o.layout()
	work := workArea(mon)
	off := o.scale(offsetPt)
	x, y := o.cur.at.X+off, o.cur.at.Y+off
	if x+w > work.Right {
		x = work.Right - w
	}
	if x < work.Left {
		x = work.Left
	}
	if y+h > work.Bottom { // does not fit below the cursor, so place it above
		y = o.cur.at.Y - off - h
	}
	if y < work.Top {
		y = work.Top
	}

	o.alpha = alphaFull
	procSetLayeredWindowAttribute.Call(o.hwnd, 0, uintptr(o.alpha), lwaAlpha)
	procSetWindowPos.Call(o.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoActivate)
	procInvalidateRect.Call(o.hwnd, 0, 1)
	procShowWindow.Call(o.hwnd, swShowNoActivate)
	procSetTimer.Call(o.hwnd, timerHide, uintptr(o.cur.duration.Milliseconds()), 0)
}

func (o *OSD) onTimer(id uintptr) {
	switch id {
	case timerHide:
		procKillTimer.Call(o.hwnd, timerHide)
		procSetTimer.Call(o.hwnd, timerFade, fadeEvery, 0)
	case timerFade:
		o.alpha -= fadeStep
		if o.alpha <= 0 {
			procKillTimer.Call(o.hwnd, timerFade)
			procShowWindow.Call(o.hwnd, swHide)
			return
		}
		procSetLayeredWindowAttribute.Call(o.hwnd, 0, uintptr(o.alpha), lwaAlpha)
	}
}

// layout measures the text with the current fonts and returns the window size.
func (o *OSD) layout() (int32, int32) {
	hdc, _, _ := procGetDC.Call(o.hwnd)
	defer procReleaseDC.Call(o.hwnd, hdc)
	pad, maxW := o.scale(paddingPt), o.scale(maxWidthPt)

	procSelectObject.Call(hdc, o.titleFont)
	tr := rect{Right: maxW}
	drawText(hdc, o.cur.title, &tr, dtCalcRect|dtSingleLine|dtNoPrefix|dtEndEllipsis)

	procSelectObject.Call(hdc, o.bodyFont)
	line := rect{Right: maxW}
	drawText(hdc, "Ag", &line, dtCalcRect|dtSingleLine|dtNoPrefix)
	br := rect{Right: maxW}
	drawText(hdc, o.cur.body, &br, dtCalcRect|dtWordBreak|dtExpandTabs|dtNoPrefix|dtEditControl)
	if limit := line.Bottom * maxBodyRows; br.Bottom > limit {
		br.Bottom = limit
	}
	if tr.Right > maxW {
		tr.Right = maxW
	}

	w := max(tr.Right, br.Right)
	gap := o.scale(3)
	o.titleRect = rect{pad, pad, pad + w, pad + tr.Bottom}
	o.bodyRect = rect{pad, o.titleRect.Bottom + gap, pad + w, o.titleRect.Bottom + gap + br.Bottom}
	return w + 2*pad, o.bodyRect.Bottom + pad
}

func (o *OSD) paint() {
	var ps [72]byte // PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(o.hwnd, uintptr(unsafe.Pointer(&ps[0])))
	defer procEndPaint.Call(o.hwnd, uintptr(unsafe.Pointer(&ps[0])))

	full := rect{0, 0, 1 << 15, 1 << 15}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), o.bgBrush)
	procSetBkMode.Call(hdc, 1) // TRANSPARENT

	procSelectObject.Call(hdc, o.titleFont)
	procSetTextColor.Call(hdc, titleColor)
	r := o.titleRect
	drawText(hdc, o.cur.title, &r, dtSingleLine|dtNoPrefix|dtEndEllipsis)

	procSelectObject.Call(hdc, o.bodyFont)
	procSetTextColor.Call(hdc, textColor)
	r = o.bodyRect
	drawText(hdc, o.cur.body, &r, dtWordBreak|dtExpandTabs|dtNoPrefix|dtEditControl)
}

// useDPI recreates the fonts if the tooltip appears on a monitor with a different scale.
func (o *OSD) useDPI(dpi uint32) {
	if dpi == o.dpi && o.bodyFont != 0 {
		return
	}
	for _, f := range []uintptr{o.titleFont, o.bodyFont} {
		if f != 0 {
			procDeleteObject.Call(f)
		}
	}
	o.dpi = dpi
	o.titleFont = newFont(-o.scale(8), 400)
	o.bodyFont = newFont(-o.scale(10), 400)
}

// scale converts points (1/72 inch) to pixels of the current monitor.
func (o *OSD) scale(pt int32) int32 { return pt * int32(o.dpi) / 72 }

func newFont(height int32, weight int) uintptr {
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	f, _, _ := procCreateFont.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face))) // DEFAULT_CHARSET, CLEARTYPE_QUALITY
	return f
}

func drawText(hdc uintptr, s string, r *rect, format uintptr) {
	p, err := syscall.UTF16FromString(s)
	if err != nil { // NUL inside the text
		p, _ = syscall.UTF16FromString("…")
	}
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)-1), uintptr(unsafe.Pointer(r)), format)
}

func monitorDPI(mon uintptr) uint32 {
	var x, y uint32
	if hr, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); int32(hr) < 0 || x == 0 {
		return 96
	}
	return x
}

func workArea(mon uintptr) rect {
	mi := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
	return mi.Work
}
