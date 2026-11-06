// Package settings is a minimal window for editing the config: fields are bound to config.Config fields.
//
// The window lives in its own thread with a message loop (IsDialogMessage provides Tab, Enter and Esc).
// Each tab is a child dialog with the tab texture (EnableThemeDialogTexture), so the label background
// matches the tab background. Only one window can be open: a repeated Show brings it to the front.
package settings

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Spell6inder/selkit/internal/about"
	"github.com/Spell6inder/selkit/internal/actions"
	"github.com/Spell6inder/selkit/internal/config"
	"github.com/Spell6inder/selkit/internal/i18n"
	"github.com/Spell6inder/selkit/internal/icon"
	"github.com/Spell6inder/selkit/internal/plugin"
	"github.com/Spell6inder/selkit/internal/win"
)

type Options struct {
	Current   func() *config.Config        // fresh config at the time of opening
	Apply     func(c *config.Config) error // save and apply (already validated)
	OpenFile  func()                       // open config.json
	Languages []string                     // codes of languages that have a translation
	Version   string
}

var (
	mu   sync.Mutex
	open uintptr // hwnd of the open window
)

// Show opens the settings window; it may be called from any thread.
func Show(o Options) {
	mu.Lock()
	defer mu.Unlock()
	if open != 0 {
		win.ForceForeground(open)
		return
	}
	ready := make(chan uintptr)
	go run(o, ready)
	open = <-ready
}

const (
	idSave       = 1 // IDOK: Enter
	idCancel     = 2 // IDCANCEL: Esc and the close button
	idFile       = 3
	idFolder     = 4
	idProgram    = 5
	idSite       = 6
	idPlugins    = 7
	idInstall    = 8
	idUpdates    = 9
	idAddRunning = 100 // + list index on the "Programs" tab
)

type window struct {
	o          Options
	hwnd, tab  uintptr
	dpi        int
	font, bold uintptr
	big        uintptr
	pages      []*page
	cur        int
}

func run(o Options, ready chan<- uintptr) {
	runtime.LockOSThread()
	enableVisualStyles()
	plugs, plugErr := plugin.Load(actions.Dir())
	plugs = append(actions.Modules(), plugs...)
	w := &window{o: o, pages: pages(o.Languages, plugs, plugErr)}
	if err := w.create(); err != nil {
		win.MessageBox("Selkit", err.Error())
		ready <- 0
		return
	}
	ready <- w.hwnd

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		// Ctrl+Tab / Ctrl+Shift+Tab switches to the adjacent tab, as in standard Windows dialogs.
		if m.Message == wmKeyDown && m.WParam == vkTab && win.KeyDown(win.VKControl) {
			step := 1
			if win.KeyDown(win.VKShift) {
				step = len(w.pages) - 1
			}
			w.selectPage((w.cur + step) % len(w.pages))
			continue
		}
		if d, _, _ := procIsDialogMessage.Call(w.hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	for _, f := range []uintptr{w.font, w.bold, w.big} {
		procDeleteObject.Call(f)
	}
	mu.Lock()
	open = 0
	mu.Unlock()
}

var (
	registerOnce sync.Once
	wndProcCb    = syscall.NewCallback(wndProc)
	dlgProcCb    = syscall.NewCallback(dlgProc)
)

// Sizes in pixels at 96 DPI.
const (
	margin   = 12
	pad      = 14  // padding inside the tab
	pageW    = 600 // width of the tab content
	labelW   = 300
	rowH     = 30
	ctrlH    = 24
	listH    = 120
	buttonW  = 110
	logoSize = 64 // the mark on the About tab
)

func (w *window) create() error {
	hInst, _, _ := procGetModuleHandle.Call(0)
	class := utf16("selkit-settings")
	var regErr error
	registerOnce.Do(func() {
		cursor, _, _ := procLoadCursor.Call(0, idcArrow)
		wc := wndClassEx{WndProc: wndProcCb, Instance: hInst, Cursor: cursor, Bg: colorBtnFace + 1, ClassName: class}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			regErr = fmt.Errorf("RegisterClassEx: %w", err)
		}
	})
	if regErr != nil {
		return regErr
	}

	// Create it where the cursor is to learn the DPI of that monitor; the size is set afterwards.
	pt := win.CursorPos()
	hwnd, _, err := procCreateWindowEx.Call(wsExControlParent, uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(utf16("Selkit — "+i18n.T("Settings")))),
		wsCaption|wsSysMenu|wsMinimizeBox, uintptr(pt.X), uintptr(pt.Y), 100, 100, 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	w.hwnd = hwnd
	windows.Store(hwnd, w)
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	w.dpi = max(int(dpi), 96)
	for _, ic := range []struct{ kind, metric uintptr }{{iconBig, smCxIcon}, {iconSmall, smCxSmIcon}} {
		size, _, _ := procGetSystemMetricsForDpi.Call(ic.metric, uintptr(w.dpi))
		procSendMessage.Call(hwnd, wmSetIcon, ic.kind, icon.Handle(int(size)))
	}
	w.font = w.makeFont(9, false)
	w.bold = w.makeFont(9, true)
	w.big = w.makeFont(16, true)

	// Tabs: first the tab strip itself, to learn how much space it takes.
	w.tab = w.childOf(hwnd, 0, "SysTabControl32", "", wsTabStop|wsClipSiblings, 0, margin, margin, pageW+2*pad, 300)
	for i, p := range w.pages {
		item := tcItem{Mask: tcifText, Text: utf16(p.title)}
		procSendMessage.Call(w.tab, tcmInsertItem, uintptr(i), uintptr(unsafe.Pointer(&item)))
	}
	cfg := w.o.Current()
	pageH := 0
	for _, p := range w.pages {
		pageH = max(pageH, w.buildPage(p, cfg))
	}
	pageH += 2 * pad

	// Tab control height = content + tab strip and border (TCM_ADJUSTRECT tells how much the border takes).
	probe := rect{0, 0, int32(w.px(pageW + 2*pad)), 1000}
	procSendMessage.Call(w.tab, tcmAdjustRect, 0, uintptr(unsafe.Pointer(&probe)))
	tabH := w.px(pageH) + (1000 - int(probe.Bottom-probe.Top))
	procSetWindowPos.Call(w.tab, 0, uintptr(w.px(margin)), uintptr(w.px(margin)),
		uintptr(w.px(pageW+2*pad)), uintptr(tabH), swpNoZOrder)
	area := rect{int32(w.px(margin)), int32(w.px(margin)), int32(w.px(margin + pageW + 2*pad)), int32(w.px(margin) + tabH)}
	procSendMessage.Call(w.tab, tcmAdjustRect, 0, uintptr(unsafe.Pointer(&area)))
	for _, p := range w.pages {
		procSetWindowPos.Call(p.hwnd, hwndTop, uintptr(area.Left), uintptr(area.Top),
			uintptr(area.Right-area.Left), uintptr(area.Bottom-area.Top), 0)
	}
	w.selectPage(0)

	// Buttons under the tabs (in logical pixels, so convert the tab height back).
	y := margin + tabH*96/w.dpi + 10
	width := pageW + 2*pad + 2*margin
	w.button(hwnd, idFile, i18n.T("Open config.json"), margin, y, 160, false)
	w.button(hwnd, idSave, i18n.T("Save"), width-margin-2*buttonW-8, y, buttonW, true)
	w.button(hwnd, idCancel, i18n.T("Cancel"), width-margin-buttonW, y, buttonW, false)
	height := y + ctrlH + 4 + margin

	// Client area size -> window size; centered on the monitor with the cursor.
	r := rect{0, 0, int32(w.px(width)), int32(w.px(height))}
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), wsCaption|wsSysMenu|wsMinimizeBox, 0, 0, uintptr(w.dpi))
	ww, wh := r.Right-r.Left, r.Bottom-r.Top
	mon, _, _ := procMonitorFromPoint.Call(pt.Packed(), monitorDefaultToNearest)
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
	wa := mi.Work
	x := wa.Left + (wa.Right-wa.Left-ww)/2
	yy := max(wa.Top+(wa.Bottom-wa.Top-wh)/2, wa.Top)
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(yy), uintptr(ww), uintptr(wh), swpNoZOrder|swpShowWindow)
	win.ForceForeground(hwnd)
	return nil
}

// buildPage creates a tab dialog with its fields; it returns the content height (in logical pixels).
func (w *window) buildPage(p *page, cfg *config.Config) int {
	hInst, _, _ := procGetModuleHandle.Call(0)
	// Empty dialog template: WS_CHILD | DS_CONTROL, no items; we add them ourselves.
	tmpl := [6]uint32{wsChild | dsControl, wsExControlParent}
	p.hwnd, _, _ = procCreateDialogIndirectParam.Call(hInst, uintptr(unsafe.Pointer(&tmpl[0])), w.hwnd, dlgProcCb, 0)
	procEnableThemeDialogTexture.Call(p.hwnd, etdtEnableTab)
	if p.about {
		return w.buildAbout(p)
	}

	ctrlW := pageW - labelW
	y := pad
	for i, f := range p.fields {
		switch f.kind {
		case kHeader:
			if i > 0 {
				y += 8
			}
			w.static(p.hwnd, f.label, pad, y, pageW, w.bold)
		case kNote:
			y += 8
			h := 4 * 18
			w.paragraph(p.hwnd, f.label, pad, y, pageW, h)
			y += h - rowH
		case kCheck:
			f.hwnd = w.childOf(p.hwnd, 0, "BUTTON", f.label, wsTabStop|bsAutoCheckbox, 0, pad, y, pageW, ctrlH)
			if *f.b(cfg) {
				procSendMessage.Call(f.hwnd, bmSetCheck, 1, 0)
			}
		case kChoice:
			w.static(p.hwnd, f.label, pad, y, labelW, w.font)
			f.hwnd = w.childOf(p.hwnd, 0, "COMBOBOX", "", wsTabStop|wsVScroll|cbsDropDownList, 0, pad+labelW, y, ctrlW, 300)
			cur, sel := *f.s(cfg), 0
			for i, o := range f.opts {
				procSendMessage.Call(f.hwnd, cbAddString, 0, uintptr(unsafe.Pointer(utf16(o.label))))
				if o.value == cur {
					sel = i
				}
			}
			procSendMessage.Call(f.hwnd, cbSetCurSel, uintptr(sel), 0)
		case kNumber:
			w.static(p.hwnd, f.label, pad, y, labelW, w.font)
			f.hwnd = w.childOf(p.hwnd, 0, "EDIT", strconv.Itoa(*f.n(cfg)), wsTabStop|esNumber|esAutoHScroll, wsExClientEdge,
				pad+labelW, y, 100, ctrlH)
		case kPlugin:
			ps := cfg.Plugin(f.plug.ID)
			f.hwnd = w.childOf(p.hwnd, 0, "BUTTON", f.label, wsTabStop|bsAutoCheckbox, 0, pad, y, labelW, ctrlH)
			setCheck(f.hwnd, !ps.Disabled)
			if f.plug.Builtin { // runs in the program, so its preview costs nothing and is always on
				w.static(p.hwnd, i18n.T("built-in"), pad+labelW, y+3, pageW-labelW, w.font)
			} else {
				f.hwnd2 = w.childOf(p.hwnd, 0, "BUTTON", i18n.T("Preview in menu"), wsTabStop|bsAutoCheckbox, 0, pad+labelW, y, pageW-labelW, ctrlH)
				setCheck(f.hwnd2, preview(f.plug, ps))
			}
			y += 22
			w.static(p.hwnd, pluginDetail(f.plug), pad+20, y, pageW-20, w.font)
		case kButton:
			y += 4
			w.childOf(p.hwnd, f.id, "BUTTON", f.label, wsTabStop|bsPushButton, 0, pad, y, 200, ctrlH+4)
		case kInstall:
			y += 8
			w.static(p.hwnd, f.label, pad, y, pageW, w.font)
			y += 22
			f.hwnd = w.childOf(p.hwnd, 0, "EDIT", "", wsTabStop|esAutoHScroll, wsExClientEdge, pad, y, pageW-130, ctrlH)
			w.childOf(p.hwnd, idInstall, "BUTTON", i18n.T("Install"), wsTabStop|bsPushButton, 0, pad+pageW-120, y-2, 120, ctrlH+4)
		case kList:
			// Two columns of two lists each; next to the title is an "add from running" button.
			col, half := i%2, (pageW-pad)/2
			x := pad + col*(half+pad)
			if col == 1 {
				y -= rowH + listH + 8
			}
			w.static(p.hwnd, f.label, x, y, half-120, w.font)
			w.childOf(p.hwnd, idAddRunning+i, "BUTTON", i18n.T("Add running…"), wsTabStop|bsPushButton, 0, x+half-116, y, 116, ctrlH)
			text := strings.Join(*f.l(cfg), "\r\n")
			f.hwnd = w.childOf(p.hwnd, 0, "EDIT", text, wsTabStop|wsVScroll|esMultiline|esAutoVScroll|esWantReturn, wsExClientEdge,
				x, y+rowH, half, listH)
			y += listH + 8
		}
		y += rowH
	}
	return y - pad
}

func (w *window) buildAbout(p *page) int {
	y := pad
	// The mark, with the name and version beside it.
	logo := w.childOf(p.hwnd, 0, "STATIC", "", ssIcon|ssRealSizeControl, 0, pad, y, logoSize, logoSize)
	procSendMessage.Call(logo, stmSetIcon, icon.Handle(w.px(logoSize)), 0)
	w.static(p.hwnd, "Selkit", pad+logoSize+16, y+4, pageW-logoSize-16, w.big)
	w.static(p.hwnd, i18n.Tf("Version %s", w.o.Version), pad+logoSize+16, y+40, pageW-logoSize-16, w.font)
	y += logoSize + 16
	desc := i18n.T("Tools for selected text on the middle mouse button: paste it Linux-style without touching " +
		"the clipboard, fix the keyboard layout, change case, transliterate and more.")
	w.paragraph(p.hwnd, desc, pad, y, pageW, 3*18)
	y += 3*18 + 12
	w.static(p.hwnd, i18n.Tf("Author: %s", about.Credit), pad, y, pageW, w.bold)
	y += rowH
	site := strings.TrimSuffix(strings.TrimPrefix(about.Site, "https://"), "/")
	link := w.childOf(p.hwnd, idSite, "SysLink", "<a>"+site+"</a>", wsTabStop, 0, pad, y+4, pageW, ctrlH)
	procSendMessage.Call(link, wmSetFont, w.font, 0)
	y += rowH
	w.static(p.hwnd, "© 2026 "+about.Credit, pad, y, pageW, w.font)
	y += rowH + 8
	exe, _ := os.Executable()
	for _, row := range []struct {
		label, path string
		id          int
		button      string
	}{
		{i18n.T("Program:"), exe, idProgram, i18n.T("Show in folder")},
		{i18n.T("Settings, log and own translations:"), config.Dir(), idFolder, i18n.T("Open folder")},
	} {
		w.static(p.hwnd, row.label, pad, y, pageW, w.font)
		y += 22
		w.static(p.hwnd, row.path, pad, y, pageW-170, w.font)
		w.childOf(p.hwnd, row.id, "BUTTON", row.button, wsTabStop|bsPushButton, 0, pad+pageW-160, y, 160, ctrlH+4)
		y += rowH + 8
	}
	return y - 8 - pad
}

func (w *window) selectPage(i int) {
	w.cur = i
	procSendMessage.Call(w.tab, tcmSetCurSel, uintptr(i), 0)
	for j, p := range w.pages {
		show := uintptr(swHide)
		if j == i {
			show = swShowNA
		}
		procShowWindow.Call(p.hwnd, show)
	}
}

func (w *window) static(parent uintptr, text string, x, y, width int, font uintptr) {
	h := w.childOf(parent, 0, "STATIC", text, ssCenterImage|ssEndEllipsis|ssNoPrefix, 0, x, y, width, ctrlH)
	procSendMessage.Call(h, wmSetFont, font, 0)
}

func (w *window) button(parent uintptr, id int, label string, x, y, width int, def bool) {
	style := uintptr(wsTabStop | bsPushButton)
	if def {
		style = wsTabStop | bsDefPushButton
	}
	w.childOf(parent, id, "BUTTON", label, style, 0, x, y, width, ctrlH+4)
}

// childOf creates a control; its id arrives in WM_COMMAND (for child windows it is passed instead of a menu).
func (w *window) childOf(parent uintptr, id int, class, text string, style, exStyle uintptr, x, y, width, height int) uintptr {
	hInst, _, _ := procGetModuleHandle.Call(0)
	h, _, _ := procCreateWindowEx.Call(exStyle, uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))),
		wsChild|wsVisible|style, uintptr(w.px(x)), uintptr(w.px(y)), uintptr(w.px(width)), uintptr(w.px(height)),
		parent, uintptr(id), hInst, 0)
	procSendMessage.Call(h, wmSetFont, w.font, 0)
	return h
}

func (w *window) px(v int) int { return v * w.dpi / 96 }

func (w *window) makeFont(pt int, bold bool) uintptr {
	weight := uintptr(400)
	if bold {
		weight = 600
	}
	h := -int32(pt * w.dpi / 72)
	f, _, _ := procCreateFont.Call(uintptr(h), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	return f
}

func (w *window) fields() []*field {
	var all []*field
	for _, p := range w.pages {
		all = append(all, p.fields...)
	}
	return all
}

// read collects the config from the fields on top of a fresh one (so settings absent from the window are not overwritten).
func (w *window) read() (*config.Config, error) {
	c := *w.o.Current()
	for _, f := range w.fields() {
		switch f.kind {
		case kCheck:
			r, _, _ := procSendMessage.Call(f.hwnd, bmGetCheck, 0, 0)
			*f.b(&c) = r == 1
		case kChoice:
			if i, _, _ := procSendMessage.Call(f.hwnd, cbGetCurSel, 0, 0); int(i) >= 0 && int(i) < len(f.opts) {
				*f.s(&c) = f.opts[i].value
			}
		case kNumber:
			v, err := strconv.Atoi(strings.TrimSpace(text(f.hwnd)))
			if err != nil || v < 0 || v > 600000 {
				return nil, fmt.Errorf("%s: %s", f.label, i18n.T("enter a number of milliseconds"))
			}
			*f.n(&c) = v
		case kList:
			*f.l(&c) = lines(text(f.hwnd))
		case kPlugin:
			ps := c.Plugin(f.plug.ID)
			ps.Disabled = !checked(f.hwnd)
			ps.Preview = nil
			if f.plug.Builtin {
				c.SetPlugin(f.plug.ID, ps)
				continue
			}
			if on := checked(f.hwnd2); on != f.plug.Preview { // stored only when it differs from the plugin's own default
				ps.Preview = &on
			}
			c.SetPlugin(f.plug.ID, ps)
		}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func lines(s string) []string {
	items := []string{}
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			items = append(items, line)
		}
	}
	return items
}

func (w *window) command(id int, from uintptr) {
	switch {
	case id == idSave:
		c, err := w.read()
		if err == nil {
			err = w.o.Apply(c)
		}
		if err != nil {
			messageBox(w.hwnd, i18n.T("Could not save the settings:")+"\n"+err.Error())
			return
		}
		procDestroyWindow.Call(w.hwnd)
	case id == idCancel:
		procDestroyWindow.Call(w.hwnd)
	case id == idFile:
		if w.o.OpenFile != nil {
			w.o.OpenFile()
		}
	case id == idFolder:
		win.ShellOpen(config.Dir())
	case id == idPlugins:
		os.MkdirAll(actions.Dir(), 0o755)
		win.ShellOpen(actions.Dir())
	case id == idInstall:
		for _, f := range w.fields() {
			if f.kind == kInstall {
				w.installPlugin(strings.TrimSpace(text(f.hwnd)))
			}
		}
	case id == idUpdates:
		w.updatePlugins()
	case id == idSite:
		win.ShellOpen(about.Site)
	case id == idProgram:
		if exe, err := os.Executable(); err == nil {
			exec.Command("explorer.exe", "/select,"+exe).Start()
		}
	case id >= idAddRunning:
		for _, p := range w.pages {
			if i := id - idAddRunning; i < len(p.fields) && p.fields[i].kind == kList {
				w.addRunning(p.fields[i], from)
				return
			}
		}
	}
}

// windows maps the main window hwnd to window, because window procedure callbacks are shared.
var windows sync.Map

func owner(hwnd uintptr) *window {
	if v, ok := windows.Load(hwnd); ok {
		return v.(*window)
	}
	return nil
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCommand:
		// Buttons (and Enter/Esc from IsDialogMessage, i.e. IDOK/IDCANCEL) have non-zero IDs; other controls have 0.
		if w := owner(hwnd); w != nil && wParam>>16 == bnClicked && wParam&0xFFFF != 0 {
			w.command(int(wParam&0xFFFF), lParam)
			return 0
		}
	case wmNotify:
		if w := owner(hwnd); w != nil {
			hdr := (*nmhdr)(unsafe.Pointer(lParam))
			if hdr.From == w.tab && hdr.Code == tcnSelChange {
				i, _, _ := procSendMessage.Call(w.tab, tcmGetCurSel, 0, 0)
				w.selectPage(int(i))
			}
		}
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		windows.Delete(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// dlgProc is the procedure of the tab dialogs: button presses inside a tab are forwarded to the main window.
func dlgProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmInitDialog:
		return 1
	case wmNotify:
		// The site link on the About tab: click or Enter.
		hdr := (*nmhdr)(unsafe.Pointer(lParam))
		if hdr.ID == idSite && (hdr.Code == nmClick || hdr.Code == nmReturn) {
			parent, _, _ := procGetParent.Call(hwnd)
			if w := owner(parent); w != nil {
				w.command(idSite, hdr.From)
			}
			return 1
		}
	case wmCommand:
		parent, _, _ := procGetParent.Call(hwnd)
		if w := owner(parent); w != nil && wParam>>16 == bnClicked && wParam&0xFFFF != 0 {
			w.command(int(wParam&0xFFFF), lParam)
			return 1
		}
	}
	return 0
}

func text(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLength.Call(hwnd)
	buf := make([]uint16, n+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func setText(hwnd uintptr, s string) {
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(utf16(s))))
}

func messageBox(owner uintptr, s string) {
	procMessageBox.Call(owner, uintptr(unsafe.Pointer(utf16(s))), uintptr(unsafe.Pointer(utf16("Selkit"))), mbIconWarning)
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

var visualStyles sync.Once

// enableVisualStyles enables Common Controls v6 (modern look of buttons, fields and tabs) for the current thread.
// selkit.exe declares it in its embedded manifest; without one (tests, "go run") we create an activation
// context from a small manifest in a temp directory.
func enableVisualStyles() {
	visualStyles.Do(func() {
		if hasManifest() {
			return
		}
		path := filepath.Join(os.TempDir(), "selkit-comctl6.manifest")
		if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
			return
		}
		ctx := actCtx{Source: utf16(path)}
		ctx.Size = uint32(unsafe.Sizeof(ctx))
		h, _, _ := procCreateActCtx.Call(uintptr(unsafe.Pointer(&ctx)))
		if h != invalidHandle {
			actCtxHandle = h
		}
	})
	if actCtxHandle != 0 {
		var cookie uintptr
		procActivateActCtx.Call(actCtxHandle, uintptr(unsafe.Pointer(&cookie)))
	}
	// comctl32 must be loaded within this context so that window classes come from version 6.
	icc := struct{ Size, ICC uint32 }{8, iccStandardClasses | iccTabClasses | iccLinkClass}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

// hasManifest reports whether the exe carries an application manifest (resource 1 of type RT_MANIFEST).
func hasManifest() bool {
	hInst, _, _ := procGetModuleHandle.Call(0)
	r, _, _ := procFindResource.Call(hInst, 1, rtManifest)
	return r != 0
}

var actCtxHandle uintptr

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <dependency><dependentAssembly>
    <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0"
      processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
  </dependentAssembly></dependency>
</assembly>`

// paragraph lays out text on several lines. Standard controls here wrap Cyrillic in the middle of a word,
// so we split into lines ourselves, measuring words with the same font.
func (w *window) paragraph(parent uintptr, text string, x, y, width, height int) {
	w.childOf(parent, 0, "STATIC", w.wrap(text, w.px(width)), ssNoPrefix, 0, x, y, width, height)
}

func (w *window) wrap(text string, maxPx int) string {
	dc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, dc)
	old, _, _ := procSelectObject.Call(dc, w.font)
	defer procSelectObject.Call(dc, old)
	measure := func(s string) int {
		u, _ := syscall.UTF16FromString(s)
		var size struct{ CX, CY int32 }
		procGetTextExtentPoint32.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&size)))
		return int(size.CX)
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && measure(line+" "+word) > maxPx {
			out = append(out, line)
			line = word
			continue
		}
		line = strings.TrimSpace(line + " " + word)
	}
	return strings.Join(append(out, line), "\r\n")
}

func setCheck(hwnd uintptr, on bool) {
	if on {
		procSendMessage.Call(hwnd, bmSetCheck, 1, 0)
	}
}

func checked(hwnd uintptr) bool {
	r, _, _ := procSendMessage.Call(hwnd, bmGetCheck, 0, 0)
	return r == 1
}

func preview(p *plugin.Plugin, ps config.PluginSettings) bool {
	if ps.Preview != nil {
		return *ps.Preview
	}
	return p.Preview
}

// pluginDetail is the line under a plugin: its description or, without one, its actions.
func pluginDetail(p *plugin.Plugin) string {
	if p.Description != "" {
		return p.Description
	}
	names := make([]string, len(p.Actions))
	for i, a := range p.Actions {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}
