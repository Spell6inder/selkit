package settings

import (
	"os"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/win"
)

type running struct{ exe, title string }

var (
	enumMu    = make(chan struct{}, 1) // EnumWindows uses one shared callback, so one call at a time
	enumFound []running
	enumCb    = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		if own, _, _ := procGetWindow.Call(hwnd, gwOwner); own != 0 {
			return 1
		}
		if ex, _, _ := procGetWindowLongPtr.Call(hwnd, gwlExStyle); ex&wsExToolWindow != 0 {
			return 1
		}
		title := text(hwnd)
		if title == "" || win.WindowPID(hwnd) == uint32(os.Getpid()) {
			return 1
		}
		if exe := win.ProcessOf(hwnd); exe != "" {
			enumFound = append(enumFound, running{exe, title})
		}
		return 1
	})
)

// runningPrograms returns programs with visible windows (exe and first window title), sorted alphabetically.
func runningPrograms() []running {
	enumMu <- struct{}{}
	enumFound = nil
	procEnumWindows.Call(enumCb, 0)
	found := enumFound
	<-enumMu

	seen := map[string]bool{}
	var out []running
	for _, r := range found {
		if k := strings.ToLower(r.exe); !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b running) int { return strings.Compare(strings.ToLower(a.exe), strings.ToLower(b.exe)) })
	return out
}

// addRunning shows a menu of running programs under the button and appends the chosen one to the list f.
func (w *window) addRunning(f *field, button uintptr) {
	progs := runningPrograms()
	have := lines(text(f.hwnd))
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	var keep []*uint16 // item strings must stay alive while the menu is open
	for i, p := range progs {
		flags := uintptr(mfString)
		if slices.ContainsFunc(have, func(h string) bool { return strings.EqualFold(h, p.exe) }) {
			flags |= mfGrayed // already in the list
		}
		title := p.title
		if r := []rune(title); len(r) > 50 {
			title = string(r[:50]) + "…"
		}
		label := utf16(strings.ReplaceAll(p.exe+"\t"+title, "&", "&&"))
		keep = append(keep, label)
		procAppendMenu.Call(menu, flags, uintptr(i+1), uintptr(unsafe.Pointer(label)))
	}
	if len(progs) == 0 {
		label := utf16(i18n.T("No programs with windows found"))
		keep = append(keep, label)
		procAppendMenu.Call(menu, mfString|mfGrayed, 0, uintptr(unsafe.Pointer(label)))
	}
	var r rect
	procGetWindowRect.Call(button, uintptr(unsafe.Pointer(&r)))
	id, _, _ := procTrackPopupMenuEx.Call(menu, tpmReturnCmd|tpmRightAlign, uintptr(r.Right), uintptr(r.Bottom), w.hwnd, 0)
	runtime.KeepAlive(keep)
	if id == 0 || int(id) > len(progs) {
		return
	}
	setText(f.hwnd, strings.Join(append(have, progs[id-1].exe), "\r\n"))
}
