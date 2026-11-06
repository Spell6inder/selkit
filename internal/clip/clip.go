// Package clip saves, replaces and restores clipboard contents.
package clip

import (
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard           = user32.NewProc("OpenClipboard")
	procCloseClipboard          = user32.NewProc("CloseClipboard")
	procEmptyClipboard          = user32.NewProc("EmptyClipboard")
	procEnumClipboardFormats    = user32.NewProc("EnumClipboardFormats")
	procGetClipboardData        = user32.NewProc("GetClipboardData")
	procSetClipboardData        = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvail  = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequenceNum = user32.NewProc("GetClipboardSequenceNumber")
	procRegisterClipboardFormat = user32.NewProc("RegisterClipboardFormatW")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// Item is a single clipboard format copied into process memory.
type Item struct {
	format uint32
	data   []byte
}

// GDI formats are not stored as HGLOBAL; images still survive via the synthesized CF_DIB/CF_DIBV5.
var gdiFormats = map[uint32]bool{
	2: true, 3: true, 9: true, 14: true, // CF_BITMAP, CF_METAFILEPICT, CF_PALETTE, CF_ENHMETAFILE
	0x80: true, 0x82: true, 0x83: true, 0x8E: true, // CF_OWNERDISPLAY, CF_DSP*
}

// Marker formats: clipboard managers, Win+V history and the cloud clipboard ignore such content.
var (
	markerOnce sync.Once
	markers    []uint32
)

func markerFormats() []uint32 {
	markerOnce.Do(func() {
		for _, name := range []string{
			"ExcludeClipboardContentFromMonitorProcessing",
			"CanIncludeInClipboardHistory",
			"CanUploadToCloudClipboard",
		} {
			u := utf16.Encode([]rune(name + "\x00"))
			if f, _, _ := procRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(&u[0]))); f != 0 {
				markers = append(markers, uint32(f))
			}
		}
	})
	return markers
}

func open() bool { return openAs(0) }

// openAs opens the clipboard on behalf of the owner window (after EmptyClipboard it becomes the owner).
func openAs(owner uintptr) bool {
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(owner); r != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// SequenceNumber changes on every write to the clipboard.
func SequenceNumber() uintptr {
	s, _, _ := procGetClipboardSequenceNum.Call()
	return s
}

// maxFormatSize: formats larger than this are not saved (so we do not keep a copy of hundreds of MB in memory).
const maxFormatSize = 64 << 20

// Snapshot is the saved clipboard contents.
type Snapshot struct {
	Items   []Item
	Skipped int           // how many formats were skipped because of size
	Took    time.Duration // applications with delayed rendering (Excel etc.) generate formats on demand, which can be slow
}

// Save copies all HGLOBAL formats of the current clipboard contents.
func Save() (Snapshot, bool) {
	t0 := time.Now()
	items, skipped, ok := save()
	return Snapshot{items, skipped, time.Since(t0)}, ok
}

func save() (items []Item, skipped int, ok bool) {
	if !open() {
		return nil, 0, false
	}
	defer procCloseClipboard.Call()

	for f := uintptr(0); ; {
		f, _, _ = procEnumClipboardFormats.Call(f)
		if f == 0 {
			break
		}
		if gdiFormats[uint32(f)] {
			continue
		}
		h, _, _ := procGetClipboardData.Call(f)
		if h == 0 {
			continue
		}
		size, _, _ := procGlobalSize.Call(h)
		if size == 0 {
			continue
		}
		if size > maxFormatSize {
			skipped++
			continue
		}
		p, _, _ := procGlobalLock.Call(h)
		if p == 0 {
			continue
		}
		data := make([]byte, size)
		copy(data, unsafe.Slice((*byte)(unsafe.Pointer(p)), size))
		procGlobalUnlock.Call(h)
		items = append(items, Item{uint32(f), data})
	}
	return items, skipped, true
}

// Set replaces the clipboard contents; private adds the "not for history/managers" markers.
func Set(items []Item, private bool) bool {
	if !open() {
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if len(items) == 0 {
		return true
	}
	if private {
		setMarkers()
	}
	for _, it := range items {
		setData(it)
	}
	return true
}

// setMarkers and setData must be called only with the clipboard open.
func setMarkers() {
	for _, f := range markerFormats() {
		setData(Item{f, make([]byte, 4)}) // DWORD 0
	}
}

func setData(it Item) {
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(it.data)))
	if h == 0 {
		return
	}
	p, _, _ := procGlobalLock.Call(h)
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(it.data)), it.data)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(uintptr(it.format), h); r == 0 {
		procGlobalFree.Call(h)
	}
}

func TextItem(s string) Item {
	u := utf16.Encode([]rune(s + "\x00"))
	return Item{cfUnicodeText, unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)}
}

// Text reads CF_UNICODETEXT or returns "".
func Text() string {
	if !open() {
		return ""
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procIsClipboardFormatAvail.Call(cfUnicodeText); r == 0 {
		return ""
	}
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	size, _, _ := procGlobalSize.Call(h)
	u := unsafe.Slice((*uint16)(unsafe.Pointer(p)), size/2)
	for i, c := range u {
		if c == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}
