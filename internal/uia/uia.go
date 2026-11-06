// Package uia is a minimal UI Automation client built on raw COM calls.
//
// All Client methods must be called from the thread where InitThread and New were called.
package uia

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/Spell6inder/selkit/internal/win"
)

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSysStringLen     = oleaut32.NewProc("SysStringLen")
	procSysFreeString    = oleaut32.NewProc("SysFreeString")
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidCUIAutomation  = guid{0xFF48DBA4, 0x60EF, 0x4201, [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	clsidCUIAutomation8 = guid{0xE22AD333, 0xB25F, 0x460C, [8]byte{0x83, 0xD0, 0x05, 0x81, 0x10, 0x73, 0x95, 0xC9}}
	iidIUIAutomation    = guid{0x30CBE57D, 0xD9D0, 0x452A, [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
	iidIUIAutomation2   = guid{0x34723AFF, 0x0C9D, 0x49D0, [8]byte{0x98, 0x96, 0x7A, 0xB5, 0x2D, 0xF8, 0xCD, 0x8A}}
	iidIUIATextPattern  = guid{0x32EBA289, 0x3583, 0x42C9, [8]byte{0x9C, 0x59, 0x3B, 0x6D, 0x9A, 0x1E, 0x9B, 0x6A}}
	iidIUIAValuePattern = guid{0xA94CD8B1, 0x0844, 0x4CD6, [8]byte{0x9D, 0x2D, 0x64, 0x05, 0x37, 0xAB, 0x39, 0xE9}}
)

const (
	textPatternID  = 10014
	valuePatternID = 10002
	editType       = 50004
	comboBoxType   = 50003
	documentType   = 50030
	hyperlinkType  = 50005
	maxAncestors   = 6
	// How many levels up to look for a button/link: the text label is usually nested inside the element itself.
	clickableDepth = 2
)

// Elements whose click does something; middle-click paste does not apply over them.
var clickableTypes = map[int32]bool{
	50000: true, // Button
	50002: true, // CheckBox
	50005: true, // Hyperlink
	50009: true, // Menu
	50010: true, // MenuBar
	50011: true, // MenuItem
	50013: true, // RadioButton
	50014: true, // ScrollBar
	50015: true, // Slider
	50019: true, // TabItem
	50027: true, // Thumb
	50031: true, // SplitButton
	50037: true, // TitleBar
}

var dragHandleTypes = map[int32]bool{
	50014: true, // ScrollBar
	50015: true, // Slider
	50027: true, // Thumb
	50037: true, // TitleBar
}

// vtable method indices (IUnknown takes 0..2).
const (
	vtQueryInterface = 0
	vtRelease        = 2

	// IUIAutomation2 (extends the 55 methods of IUIAutomation)
	vtGetConnectionTimeout  = 60
	vtPutConnectionTimeout  = 61
	vtGetTransactionTimeout = 62
	vtPutTransactionTimeout = 63

	// IUIAutomation
	vtElementFromHandle    = 6
	vtElementFromPoint     = 7
	vtGetFocusedElement    = 8
	vtGetControlViewWalker = 14

	// IUIAutomationElement
	vtGetCurrentPatternAs      = 14
	vtCurrentControlType       = 21
	vtCurrentClassName         = 30
	vtCurrentBoundingRectangle = 43

	// IUIAutomationValuePattern
	vtValueIsReadOnly = 5

	// IUIAutomationTreeWalker
	vtGetParentElement = 3

	// IUIAutomationTextPattern
	vtGetSelection = 5

	// IUIAutomationTextRangeArray
	vtArrayLength     = 3
	vtArrayGetElement = 4

	// IUIAutomationTextRange
	vtGetText = 12
)

// InitThread initializes COM (MTA) for the current OS thread.
func InitThread() { procCoInitializeEx.Call(0, 0) }

type Client struct{ p uintptr }

// New creates a UIA client. timeout limits how long to wait for an unresponsive application
// (by default UIA waits up to 20 s, and the whole worker would hang along with it).
// The second return value describes the applied timeouts or why they could not be set.
func New(timeout time.Duration) (*Client, string, error) {
	var p uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation8)), 0, 1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&p)))
	if !hrOK(hr) {
		hr, _, _ = procCoCreateInstance.Call(
			uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, 1,
			uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&p)))
		if !hrOK(hr) {
			return nil, "", fmt.Errorf("CoCreateInstance(CUIAutomation): hr=0x%08X", uint32(hr))
		}
	}
	return &Client{p}, setTimeouts(p, timeout), nil
}

func setTimeouts(p uintptr, timeout time.Duration) string {
	var p2 uintptr
	if !hrOK(comCall(p, vtQueryInterface, uintptr(unsafe.Pointer(&iidIUIAutomation2)), uintptr(unsafe.Pointer(&p2)))) || p2 == 0 {
		return "IUIAutomation2 unavailable, default timeouts"
	}
	defer release(p2)
	var conn0, tx0 uint32
	comCall(p2, vtGetConnectionTimeout, uintptr(unsafe.Pointer(&conn0)))
	comCall(p2, vtGetTransactionTimeout, uintptr(unsafe.Pointer(&tx0)))
	ms := uintptr(timeout.Milliseconds())
	comCall(p2, vtPutConnectionTimeout, ms)
	comCall(p2, vtPutTransactionTimeout, ms)
	var conn, tx uint32
	comCall(p2, vtGetConnectionTimeout, uintptr(unsafe.Pointer(&conn)))
	comCall(p2, vtGetTransactionTimeout, uintptr(unsafe.Pointer(&tx)))
	return fmt.Sprintf("UIA timeouts: connection %d→%d ms, transaction %d→%d ms", conn0, conn, tx0, tx)
}

// Selection looks for selected text: first in the focused element, then under the cursor and its ancestors.
// how describes where it came from (for the log); supported=false means there is no TextPattern nearby,
// i.e. UIA cannot help at this spot in the application.
func (c *Client) Selection(pt win.Point) (text, how string, supported bool) {
	if c == nil {
		return "", "uia unavailable", false
	}
	if el := c.focused(); el != 0 {
		desc := describe(el)
		text, ok := selectionOf(el)
		release(el)
		if text != "" {
			return text, "uia focused " + desc, true
		}
		if ok {
			return "", "uia focused " + desc + ": selection is empty", true
		}
	}

	var found, desc, firstDesc string
	var hasPattern bool
	depth := c.walkUp(pt, maxAncestors, func(el uintptr) bool {
		if firstDesc == "" {
			firstDesc = describe(el)
		}
		text, ok := selectionOf(el)
		if ok {
			found, hasPattern, desc = text, true, describe(el)
		}
		return ok
	})
	switch {
	case found != "":
		return found, fmt.Sprintf("uia point+%d %s", depth, desc), true
	case hasPattern:
		return "", fmt.Sprintf("uia point+%d %s: selection is empty", depth, desc), true
	default:
		return "", "uia: no TextPattern (" + firstDesc + ")", false
	}
}

// DragHandleAt reports whether a scrollbar, thumb or window title bar is under the point: dragging them is not a selection.
func (c *Client) DragHandleAt(pt win.Point) (int32, bool) {
	var found int32
	c.walkUp(pt, 1, func(el uintptr) bool {
		ct := controlType(el)
		if dragHandleTypes[ct] {
			found = ct
			return true
		}
		return false
	})
	return found, found != 0
}

// EditAt reports whether there is an input field under the cursor: the element itself or its ancestor, and if the field is covered
// (e.g. the Chrome address bar by the suggestions panel), the focused field that contains the point.
func (c *Client) EditAt(pt win.Point) bool {
	if c.walkUp(pt, maxAncestors, isEditable) >= 0 {
		return true
	}
	el := c.focused()
	defer release(el)
	return el != 0 && isEditable(el) && boundingRect(el).contains(pt)
}

// isEditable reports an input field or a combobox with an editable value (this is how Google search looks, for example).
func isEditable(el uintptr) bool {
	switch controlType(el) {
	case editType:
		return true
	case comboBoxType:
		readOnly, ok := valueReadOnly(el)
		return ok && !readOnly
	}
	return false
}

// valueReadOnly reads ValuePattern.IsReadOnly; ok=false means there is no pattern.
func valueReadOnly(el uintptr) (readOnly, ok bool) {
	var vp uintptr
	if !hrOK(comCall(el, vtGetCurrentPatternAs, valuePatternID,
		uintptr(unsafe.Pointer(&iidIUIAValuePattern)), uintptr(unsafe.Pointer(&vp)))) || vp == 0 {
		return false, false
	}
	defer release(vp)
	var ro int32
	if !hrOK(comCall(vp, vtValueIsReadOnly, uintptr(unsafe.Pointer(&ro)))) {
		return false, false
	}
	return ro != 0, true
}

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) contains(pt win.Point) bool {
	return pt.X >= r.Left && pt.X < r.Right && pt.Y >= r.Top && pt.Y < r.Bottom
}

// boundingRect is the element bounds in screen coordinates (the COM version returns a plain RECT).
func boundingRect(el uintptr) rect {
	var r rect
	comCall(el, vtCurrentBoundingRectangle, uintptr(unsafe.Pointer(&r)))
	return r
}

// DescribeAt returns the chain of elements under the point (the element itself and several ancestors) for log diagnostics.
func (c *Client) DescribeAt(pt win.Point) string {
	var parts []string
	c.walkUp(pt, 3, func(el uintptr) bool {
		parts = append(parts, fmt.Sprintf("%d/%q", controlType(el), className(el)))
		return false
	})
	if len(parts) == 0 {
		return "no element"
	}
	return strings.Join(parts, " ← ")
}

// ClickableAt reports whether a button, link, menu item, etc. is under the cursor (considering several ancestors).
// An input field or document on the way means this is already a text area, and the search stops.
func (c *Client) ClickableAt(pt win.Point) (int32, bool) {
	var found int32
	c.walkUp(pt, clickableDepth, func(el uintptr) bool {
		ct := controlType(el)
		if clickableTypes[ct] {
			found = ct
			return true
		}
		return ct == editType || ct == documentType
	})
	return found, found != 0
}

// Focused returns the type and class of the element with keyboard focus.
func (c *Client) Focused() (isEdit bool, class string) {
	el := c.focused()
	if el == 0 {
		return false, ""
	}
	defer release(el)
	return controlType(el) == editType, className(el)
}

func (c *Client) focused() uintptr {
	var el uintptr
	if c == nil || !hrOK(comCall(c.p, vtGetFocusedElement, uintptr(unsafe.Pointer(&el)))) {
		return 0
	}
	return el
}

// walkUp goes up from the element under the cursor (no higher than maxDepth ancestors) until match returns true.
// It returns the match depth or -1.
func (c *Client) walkUp(pt win.Point, maxDepth int, match func(el uintptr) bool) int {
	var el uintptr
	if c == nil || !hrOK(comCall(c.p, vtElementFromPoint, pt.Packed(), uintptr(unsafe.Pointer(&el)))) || el == 0 {
		return -1
	}
	var walker uintptr
	comCall(c.p, vtGetControlViewWalker, uintptr(unsafe.Pointer(&walker)))
	defer release(walker)

	for depth := 0; el != 0 && depth <= maxDepth; depth++ {
		if match(el) {
			release(el)
			return depth
		}
		var parent uintptr
		if walker != 0 {
			comCall(walker, vtGetParentElement, el, uintptr(unsafe.Pointer(&parent)))
		}
		release(el)
		el = parent
	}
	release(el)
	return -1
}

// selectionOf returns the selected text and whether the element supports TextPattern.
func selectionOf(el uintptr) (string, bool) {
	var tp uintptr
	if !hrOK(comCall(el, vtGetCurrentPatternAs, textPatternID,
		uintptr(unsafe.Pointer(&iidIUIATextPattern)), uintptr(unsafe.Pointer(&tp)))) || tp == 0 {
		return "", false
	}
	defer release(tp)

	var arr uintptr
	if !hrOK(comCall(tp, vtGetSelection, uintptr(unsafe.Pointer(&arr)))) || arr == 0 {
		return "", true
	}
	defer release(arr)

	var n int32
	comCall(arr, vtArrayLength, uintptr(unsafe.Pointer(&n)))
	var parts []string
	for i := int32(0); i < n; i++ {
		var rng uintptr
		if !hrOK(comCall(arr, vtArrayGetElement, uintptr(i), uintptr(unsafe.Pointer(&rng)))) || rng == 0 {
			continue
		}
		var bstr uintptr
		if hrOK(comCall(rng, vtGetText, ^uintptr(0), uintptr(unsafe.Pointer(&bstr)))) {
			if s := bstrToString(bstr); s != "" {
				parts = append(parts, s)
			}
		}
		release(rng)
	}
	return strings.Join(parts, "\n"), true
}

func controlType(el uintptr) int32 {
	var ct int32
	comCall(el, vtCurrentControlType, uintptr(unsafe.Pointer(&ct)))
	return ct
}

func className(el uintptr) string {
	var bstr uintptr
	comCall(el, vtCurrentClassName, uintptr(unsafe.Pointer(&bstr)))
	return bstrToString(bstr)
}

func describe(el uintptr) string {
	return fmt.Sprintf("type=%d class=%q", controlType(el), className(el))
}

func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func release(obj uintptr) {
	if obj != 0 {
		comCall(obj, vtRelease)
	}
}

func hrOK(hr uintptr) bool { return int32(hr) >= 0 }

func bstrToString(b uintptr) string {
	if b == 0 {
		return ""
	}
	defer procSysFreeString.Call(b)
	n, _, _ := procSysStringLen.Call(b)
	return string(utf16.Decode(unsafe.Slice((*uint16)(unsafe.Pointer(b)), n)))
}

// HyperlinkAt reports whether a link is under the point (the element itself or text inside it).
func (c *Client) HyperlinkAt(pt win.Point) bool {
	return c.walkUp(pt, clickableDepth, func(el uintptr) bool { return controlType(el) == hyperlinkType }) >= 0
}
