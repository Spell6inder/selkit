package settings

import "syscall"

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	uxtheme  = syscall.NewLazyDLL("uxtheme.dll")

	procRegisterClassEx           = user32.NewProc("RegisterClassExW")
	procCreateWindowEx            = user32.NewProc("CreateWindowExW")
	procDefWindowProc             = user32.NewProc("DefWindowProcW")
	procDestroyWindow             = user32.NewProc("DestroyWindow")
	procGetMessage                = user32.NewProc("GetMessageW")
	procTranslateMessage          = user32.NewProc("TranslateMessage")
	procDispatchMessage           = user32.NewProc("DispatchMessageW")
	procIsDialogMessage           = user32.NewProc("IsDialogMessageW")
	procPostQuitMessage           = user32.NewProc("PostQuitMessage")
	procSendMessage               = user32.NewProc("SendMessageW")
	procSetWindowPos              = user32.NewProc("SetWindowPos")
	procGetWindowText             = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength       = user32.NewProc("GetWindowTextLengthW")
	procLoadCursor                = user32.NewProc("LoadCursorW")
	procMessageBox                = user32.NewProc("MessageBoxW")
	procGetDpiForWindow           = user32.NewProc("GetDpiForWindow")
	procGetSystemMetricsForDpi    = user32.NewProc("GetSystemMetricsForDpi")
	procAdjustWindowRectExForDpi  = user32.NewProc("AdjustWindowRectExForDpi")
	procMonitorFromPoint          = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfo            = user32.NewProc("GetMonitorInfoW")
	procShowWindow                = user32.NewProc("ShowWindow")
	procGetDC                     = user32.NewProc("GetDC")
	procReleaseDC                 = user32.NewProc("ReleaseDC")
	procSetWindowText             = user32.NewProc("SetWindowTextW")
	procGetParent                 = user32.NewProc("GetParent")
	procGetWindowRect             = user32.NewProc("GetWindowRect")
	procCreateDialogIndirectParam = user32.NewProc("CreateDialogIndirectParamW")
	procEnumWindows               = user32.NewProc("EnumWindows")
	procIsWindowVisible           = user32.NewProc("IsWindowVisible")
	procGetWindow                 = user32.NewProc("GetWindow")
	procGetWindowLongPtr          = user32.NewProc("GetWindowLongPtrW")
	procCreatePopupMenu           = user32.NewProc("CreatePopupMenu")
	procAppendMenu                = user32.NewProc("AppendMenuW")
	procDestroyMenu               = user32.NewProc("DestroyMenu")
	procTrackPopupMenuEx          = user32.NewProc("TrackPopupMenuEx")

	procCreateFont           = gdi32.NewProc("CreateFontW")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procGetTextExtentPoint32 = gdi32.NewProc("GetTextExtentPoint32W")

	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	procCreateActCtx    = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx  = kernel32.NewProc("ActivateActCtx")
	procFindResource    = kernel32.NewProc("FindResourceW")

	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")

	procEnableThemeDialogTexture = uxtheme.NewProc("EnableThemeDialogTexture")
)

const (
	wmSetIcon  = 0x0080
	iconSmall  = 0
	iconBig    = 1
	smCxIcon   = 11
	smCxSmIcon = 49

	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsTabStop     = 0x00010000
	wsVScroll     = 0x00200000

	wsExClientEdge = 0x00000200

	bsPushButton    = 0x0
	bsDefPushButton = 0x1
	bsAutoCheckbox  = 0x3

	cbsDropDownList = 0x3

	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esAutoHScroll = 0x0080
	esWantReturn  = 0x1000
	esNumber      = 0x2000

	ssIcon            = 0x0003
	ssRealSizeControl = 0x0040 // the icon is scaled to the control instead of the control to the icon
	ssCenterImage     = 0x0200
	ssEndEllipsis     = 0x4000
	stmSetIcon        = 0x0170

	wmDestroy  = 0x0002
	wmClose    = 0x0010
	wmSetFont  = 0x0030
	wmCommand  = 0x0111
	bmGetCheck = 0x00F0
	bmSetCheck = 0x00F1
	bnClicked  = 0

	cbAddString = 0x0143
	cbGetCurSel = 0x0147
	cbSetCurSel = 0x014E

	idcArrow     = 32512
	colorBtnFace = 15
	swpNoZOrder  = 0x0004
	// Unlike the first ShowWindow call, it ignores STARTUPINFO (launch with a "minimized" or hidden window).
	swpShowWindow           = 0x0040
	monitorDefaultToNearest = 2
	mbIconWarning           = 0x30
	invalidHandle           = ^uintptr(0)

	wsExControlParent = 0x00010000
	wsExToolWindow    = 0x00000080
	wsClipSiblings    = 0x04000000
	dsControl         = 0x0400
	ssNoPrefix        = 0x0080

	wmInitDialog = 0x0110
	wmNotify     = 0x004E
	wmKeyDown    = 0x0100
	vkTab        = 0x09

	tcmGetCurSel  = 0x130B
	tcmSetCurSel  = 0x130C
	tcmAdjustRect = 0x1328
	tcmInsertItem = 0x133E // TCM_INSERTITEMW
	tcifText      = 0x0001
	tcnSelChange  = ^uint32(550) // -551

	etdtEnableTab      = 0x6
	iccStandardClasses = 0x4000
	iccTabClasses      = 0x0008
	iccLinkClass       = 0x8000
	rtManifest         = 24
	nmClick            = 0xFFFFFFFE // NM_CLICK (-2)
	nmReturn           = 0xFFFFFFFC // NM_RETURN (-4)

	swHide   = 0
	swShowNA = 8
	hwndTop  = 0

	gwOwner    = 4
	gwlExStyle = ^uintptr(19) // -20

	mfString      = 0x0000
	mfGrayed      = 0x0001
	tpmReturnCmd  = 0x0100
	tpmRightAlign = 0x0008
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type wndClassEx struct {
	Size, Style                uint32
	WndProc                    uintptr
	ClsExtra, WndExtra         int32
	Instance, Icon, Cursor, Bg uintptr
	MenuName, ClassName        *uint16
	IconSm                     uintptr
}

type actCtx struct {
	Size                  uint32
	Flags                 uint32
	Source                *uint16
	ProcessorArchitecture uint16
	LangID                uint16
	AssemblyDirectory     *uint16
	ResourceName          *uint16
	ApplicationName       *uint16
	Module                uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	_       uint32
}

type nmhdr struct {
	From uintptr
	ID   uintptr
	Code uint32
}

type tcItem struct {
	Mask, State, StateMask uint32
	Text                   *uint16
	TextMax, Image         int32
	Param                  uintptr
}
