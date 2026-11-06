// Package layout re-types text that was typed in the wrong keyboard layout ("ghbdsn" → "привіт").
//
// The tables are built from the layouts installed in the system: for each physical key we ask Windows
// which character it produces in each layout (unmodified, with Shift, AltGr and Shift+AltGr).
package layout

import (
	"strings"
	"syscall"
	"unicode"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procGetKeyboardLayoutList    = user32.NewProc("GetKeyboardLayoutList")
	procGetKeyboardLayout        = user32.NewProc("GetKeyboardLayout")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procMapVirtualKeyEx          = user32.NewProc("MapVirtualKeyExW")
	procToUnicodeEx              = user32.NewProc("ToUnicodeEx")
	procPostMessage              = user32.NewProc("PostMessageW")

	procGetLocaleInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("GetLocaleInfoW")
)

const (
	mapvkVscToVk           = 1
	toUnicodeNoStateChange = 0x4 // Windows 10 1607+: do not touch the dead-key state
	localeSISO639LangName  = 0x59
	wmInputLangChangeReq   = 0x0050

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
)

// Scan codes of keys that produce characters: the digit row, three letter rows, "\" and the extra ISO keyboard key.
var scanCodes = func() []uint32 {
	var sc []uint32
	for _, r := range [][2]uint32{{0x02, 0x0D}, {0x10, 0x1B}, {0x1E, 0x29}, {0x2B, 0x35}, {0x56, 0x56}} {
		for c := r[0]; c <= r[1]; c++ {
			sc = append(sc, c)
		}
	}
	return sc
}()

// key is a physical key with modifiers: bit 0 is Shift, bit 1 is AltGr.
type key struct {
	sc   uint32
	mods uint8
}

type Layout struct {
	HKL  uintptr
	Name string // "EN", "UK"…
	char map[key]rune
	pos  map[rune]key
}

// Installed returns the layouts in the order in which Windows switches them.
func Installed() []*Layout {
	n, _, _ := procGetKeyboardLayoutList.Call(0, 0)
	if n == 0 {
		return nil
	}
	hkls := make([]uintptr, n)
	n, _, _ = procGetKeyboardLayoutList.Call(n, uintptr(unsafe.Pointer(&hkls[0])))
	var ls []*Layout
	names := map[string]int{}
	for _, h := range hkls[:n] {
		l := build(h)
		names[l.Name]++
		if names[l.Name] > 1 { // two layouts of the same language (for example US and UK English)
			l.Name += strings.Repeat("'", names[l.Name]-1)
		}
		ls = append(ls, l)
	}
	return ls
}

func build(hkl uintptr) *Layout {
	l := &Layout{HKL: hkl, Name: langName(hkl), char: map[key]rune{}, pos: map[rune]key{}}
	var state [256]byte
	var buf [8]uint16
	for _, mods := range []uint8{0, 1, 2, 3} {
		state = [256]byte{}
		if mods&1 != 0 {
			state[vkShift] = 0x80
		}
		if mods&2 != 0 {
			state[vkControl], state[vkMenu] = 0x80, 0x80
		}
		for _, sc := range scanCodes {
			vk, _, _ := procMapVirtualKeyEx.Call(uintptr(sc), mapvkVscToVk, hkl)
			if vk == 0 {
				continue
			}
			n, _, _ := procToUnicodeEx.Call(vk, uintptr(sc), uintptr(unsafe.Pointer(&state[0])),
				uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), toUnicodeNoStateChange, hkl)
			if int32(n) != 1 || buf[0] < 0x20 || buf[0] == 0x7F {
				continue // dead key, several characters or a control character
			}
			k, r := key{sc, mods}, rune(buf[0])
			l.char[k] = r
			if _, dup := l.pos[r]; !dup { // the same character on several keys: take the simplest
				l.pos[r] = k
			}
		}
	}
	return l
}

func langName(hkl uintptr) string {
	var buf [16]uint16
	n, _, _ := procGetLocaleInfo.Call(hkl&0xFFFF, localeSISO639LangName, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "?"
	}
	return strings.ToUpper(syscall.UTF16ToString(buf[:]))
}

// Current is the layout active in window hwnd.
func Current(hwnd uintptr) uintptr {
	tid, _, _ := procGetWindowThreadProcessId.Call(hwnd, 0)
	h, _, _ := procGetKeyboardLayout.Call(tid)
	return h
}

// Activate asks the window to switch to the layout.
func Activate(hwnd, hkl uintptr) { procPostMessage.Call(hwnd, wmInputLangChangeReq, 0, hkl) }

// Detect guesses which layout the text was typed in: the one in which most of its characters have a key.
// Letters weigh more than punctuation (which is nearly everywhere); on a tie, the prefer window layout wins.
func Detect(ls []*Layout, s string, prefer uintptr) int {
	best, bestScore := -1, 0
	for i, l := range ls {
		score := 0
		for _, r := range s {
			if _, ok := l.pos[r]; ok {
				if unicode.IsLetter(r) {
					score += 2
				} else {
					score++
				}
			}
		}
		if score > bestScore || (score == bestScore && score > 0 && l.HKL == prefer) {
			best, bestScore = i, score
		}
	}
	return best
}

// Convert re-types s as if the same keys had been pressed in layout to.
// Characters absent from from (spaces, line breaks, digits with no counterpart) are kept as is.
func Convert(s string, from, to *Layout) string {
	return strings.Map(func(r rune) rune {
		k, ok := from.pos[r]
		if !ok {
			return r
		}
		if c, ok := to.char[k]; ok {
			return c
		}
		return r
	}, s)
}
