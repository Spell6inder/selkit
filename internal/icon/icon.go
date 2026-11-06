// Package icon serves the Selkit mark embedded from resources/icon (built from SVG by go generate).
package icon

import (
	"encoding/binary"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Spell6inder/selkit/resources"
)

// ICO is the mark as an ICO file with every size, for the tray.
func ICO() []byte { return resources.IconICO }

// DisabledICO is the same mark in gray, shown while catching selections is off.
func DisabledICO() []byte { return resources.IconOffICO }

var (
	procCreateIconFromResourceEx = syscall.NewLazyDLL("user32.dll").NewProc("CreateIconFromResourceEx")

	handlesMu sync.Mutex
	handles   = map[int]uintptr{}
)

// Handle returns an HICON of the active mark at size×size pixels, for WM_SETICON: the nearest frame
// not smaller than size, scaled by Windows if it is not exact. Icons are cached for the life of the process.
func Handle(size int) uintptr {
	handlesMu.Lock()
	defer handlesMu.Unlock()
	if h, ok := handles[size]; ok {
		return h
	}
	frame := pickFrame(ICO(), size)
	if frame == nil {
		return 0
	}
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&frame[0])), uintptr(len(frame)),
		1, 0x00030000, uintptr(size), uintptr(size), 0) // fIcon, version 3.0
	if h != 0 {
		handles[size] = h
	}
	return h
}

// pickFrame returns the image data (BMP or PNG) of the smallest frame at least size pixels wide,
// or of the largest frame if none is.
func pickFrame(ico []byte, size int) []byte {
	if len(ico) < 6 {
		return nil
	}
	le := binary.LittleEndian
	count := int(le.Uint16(ico[4:]))
	best, bestW := -1, 0
	for i := 0; i < count; i++ {
		e := ico[6+16*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		if bestW == 0 || (w >= size && (bestW < size || w < bestW)) || (bestW < size && w > bestW) {
			best, bestW = i, w
		}
	}
	e := ico[6+16*best:]
	n, off := le.Uint32(e[8:]), le.Uint32(e[12:])
	if int(off+n) > len(ico) {
		return nil
	}
	return ico[off : off+n]
}
