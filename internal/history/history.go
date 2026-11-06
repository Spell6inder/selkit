// Package history removes entries from the Windows clipboard history (Win+V) that appeared because of our Ctrl+C.
//
// When Ctrl+C grabs the text, the application itself writes to the clipboard, so we cannot mark the entry "not for history".
// Instead, via WinRT (Windows.ApplicationModel.DataTransfer.Clipboard, Windows 10 1809+) we find
// the just-added entry with the same text and delete it. It works even when Selkit is not focused.
package history

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	combase = syscall.NewLazyDLL("combase.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")
	user32  = syscall.NewLazyDLL("user32.dll")

	procRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
	procCoInitializeEx            = ole32.NewProc("CoInitializeEx")
	procPeekMessage               = user32.NewProc("PeekMessageW")
	procDispatchMessage           = user32.NewProc("DispatchMessageW")
)

// Clipboard in WinRT is a single-threaded class: activation from an MTA fails with RO_E_UNSUPPORTED_FROM_MTA.
const coinitApartmentThreaded = 0x2

// initThread makes the current (locked) thread an STA.
func initThread() { procCoInitializeEx.Call(0, coinitApartmentThreaded) }

// pumpMessages processes the STA message queue; results of asynchronous operations arrive through it.
func pumpMessages() {
	var m [48]byte // MSG
	for {
		if r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0, 1); r == 0 { // PM_REMOVE
			return
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m[0])))
	}
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// GUIDs and method order are verified against the WinRT metadata (Windows.ApplicationModel.DataTransfer.winmd).
var (
	iidIClipboardStatics2 = guid{0xD2AC1B6A, 0xD29F, 0x554B, [8]byte{0xB3, 0x03, 0xF0, 0x45, 0x23, 0x45, 0xFE, 0x02}}
	iidIAsyncInfo         = guid{0x00000036, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// vtable indices: IInspectable takes 0..5.
const (
	vtQueryInterface = 0
	vtRelease        = 2

	// IClipboardStatics2
	vtGetHistoryItemsAsync  = 6
	vtDeleteItemFromHistory = 8
	vtIsHistoryEnabled      = 10

	// IAsyncInfo
	vtAsyncStatus = 7
	// IAsyncOperation<T>
	vtGetResults = 8

	// IClipboardHistoryItemsResult
	vtResultStatus = 6
	vtResultItems  = 7

	// IVectorView<T>
	vtGetAt   = 6
	vtGetSize = 7

	// IClipboardHistoryItem
	vtTimestamp = 7
	vtContent   = 8

	// IDataPackageView
	vtContains     = 10
	vtGetTextAsync = 12
)

const (
	asyncStarted   = 0
	asyncCompleted = 1

	historySuccess = 0

	// Search only among the newest entries and only those that appeared just now.
	maxScan   = 3
	maxAge    = 10 * time.Second
	asyncWait = 2 * time.Second
)

// Attempts to find the entry: the history service adds it asynchronously, with a delay.
var retryDelays = []time.Duration{300 * time.Millisecond, 700 * time.Millisecond, 1500 * time.Millisecond}

// Cleaner deletes entries on a background thread so as not to slow down mouse handling.
type Cleaner struct {
	queue chan request
}

type request struct {
	text string
	at   time.Time
}

// Start launches the background WinRT thread. It returns an error if the history API is unavailable (old Windows, etc.).
func Start() (*Cleaner, error) {
	c := &Cleaner{queue: make(chan request, 16)}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		initThread()
		statics, err := activate()
		ready <- err
		if err != nil {
			return
		}
		// The STA thread must regularly process its message queue, even when there are no requests.
		pump := time.NewTicker(200 * time.Millisecond)
		defer pump.Stop()
		for {
			select {
			case r := <-c.queue:
				c.process(statics, r)
			case <-pump.C:
				pumpMessages()
			}
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return c, nil
}

// Forget asks to delete the fresh entry with this text from the history. It does not block.
func (c *Cleaner) Forget(text string) {
	select {
	case c.queue <- request{text, time.Now()}:
	default:
	}
}

func (c *Cleaner) process(statics uintptr, r request) {
	if !isEnabled(statics) {
		return
	}
	for _, d := range retryDelays {
		if wait := time.Until(r.at.Add(d)); wait > 0 {
			time.Sleep(wait)
		}
		deleted, err := deleteRecent(statics, r.text)
		if err != nil {
			log.Printf("Win+V history: %v", err)
			return
		}
		if deleted {
			log.Printf("Win+V history: removed entry from Ctrl+C (%v after copy)", time.Since(r.at).Round(time.Millisecond))
			return
		}
	}
}

func activate() (uintptr, error) {
	name, err := newHString("Windows.ApplicationModel.DataTransfer.Clipboard")
	if err != nil {
		return 0, err
	}
	defer deleteHString(name)
	var statics uintptr
	if hr, _, _ := procRoGetActivationFactory.Call(name, uintptr(unsafe.Pointer(&iidIClipboardStatics2)), uintptr(unsafe.Pointer(&statics))); !hrOK(hr) {
		return 0, fmt.Errorf("RoGetActivationFactory(IClipboardStatics2): hr=0x%08X (Windows 10 1809+ required)", uint32(hr))
	}
	return statics, nil
}

func isEnabled(statics uintptr) bool {
	var on uint8
	if !hrOK(comCall(statics, vtIsHistoryEnabled, uintptr(unsafe.Pointer(&on)))) {
		return false
	}
	return on != 0
}

// deleteRecent looks among the newest history entries for a text entry no older than maxAge and deletes it.
func deleteRecent(statics uintptr, text string) (bool, error) {
	var op uintptr
	if hr := comCall(statics, vtGetHistoryItemsAsync, uintptr(unsafe.Pointer(&op))); !hrOK(hr) {
		return false, fmt.Errorf("GetHistoryItemsAsync: hr=0x%08X", uint32(hr))
	}
	result, err := awaitObject(op)
	if err != nil {
		return false, fmt.Errorf("GetHistoryItemsAsync: %w", err)
	}
	defer release(result)

	var status int32
	comCall(result, vtResultStatus, uintptr(unsafe.Pointer(&status)))
	if status != historySuccess {
		return false, nil // disabled or no access: silently do nothing
	}
	var items uintptr
	if !hrOK(comCall(result, vtResultItems, uintptr(unsafe.Pointer(&items)))) || items == 0 {
		return false, nil
	}
	defer release(items)

	var n uint32
	comCall(items, vtGetSize, uintptr(unsafe.Pointer(&n)))
	for i := uint32(0); i < n && i < maxScan; i++ {
		var item uintptr
		if !hrOK(comCall(items, vtGetAt, uintptr(i), uintptr(unsafe.Pointer(&item)))) || item == 0 {
			continue
		}
		match := isRecentText(item, text)
		if match {
			var ok uint8
			comCall(statics, vtDeleteItemFromHistory, item, uintptr(unsafe.Pointer(&ok)))
			release(item)
			return ok != 0, nil
		}
		release(item)
	}
	return false, nil
}

func isRecentText(item uintptr, text string) bool {
	var ts int64 // DateTime: 100-ns intervals since 1601-01-01 UTC
	comCall(item, vtTimestamp, uintptr(unsafe.Pointer(&ts)))
	if time.Since(fromDateTime(ts)) > maxAge {
		return false
	}

	var content uintptr
	if !hrOK(comCall(item, vtContent, uintptr(unsafe.Pointer(&content)))) || content == 0 {
		return false
	}
	defer release(content)

	format, err := newHString("Text") // StandardDataFormats.Text
	if err != nil {
		return false
	}
	defer deleteHString(format)
	var has uint8
	if !hrOK(comCall(content, vtContains, format, uintptr(unsafe.Pointer(&has)))) || has == 0 {
		return false
	}

	var op uintptr
	if !hrOK(comCall(content, vtGetTextAsync, uintptr(unsafe.Pointer(&op)))) {
		return false
	}
	h, err := awaitHString(op)
	if err != nil {
		return false
	}
	defer deleteHString(h)
	return hstringToString(h) == text
}

// awaitObject waits for an IAsyncOperation<T> where T is an interface and returns the result (it must be released).
func awaitObject(op uintptr) (uintptr, error) {
	defer release(op)
	if err := await(op); err != nil {
		return 0, err
	}
	var res uintptr
	if hr := comCall(op, vtGetResults, uintptr(unsafe.Pointer(&res))); !hrOK(hr) {
		return 0, fmt.Errorf("GetResults: hr=0x%08X", uint32(hr))
	}
	return res, nil
}

// awaitHString waits for an IAsyncOperation<HSTRING> and returns the string (it must be deleted).
func awaitHString(op uintptr) (uintptr, error) {
	defer release(op)
	if err := await(op); err != nil {
		return 0, err
	}
	var h uintptr
	if hr := comCall(op, vtGetResults, uintptr(unsafe.Pointer(&h))); !hrOK(hr) {
		return 0, fmt.Errorf("GetResults: hr=0x%08X", uint32(hr))
	}
	return h, nil
}

// await polls IAsyncInfo.Status: simpler than implementing a COM Completed handler.
func await(op uintptr) error {
	var info uintptr
	if !hrOK(comCall(op, vtQueryInterface, uintptr(unsafe.Pointer(&iidIAsyncInfo)), uintptr(unsafe.Pointer(&info)))) || info == 0 {
		return errors.New("no IAsyncInfo")
	}
	defer release(info)
	for deadline := time.Now().Add(asyncWait); ; time.Sleep(5 * time.Millisecond) {
		pumpMessages()
		var st int32
		comCall(info, vtAsyncStatus, uintptr(unsafe.Pointer(&st)))
		switch {
		case st == asyncCompleted:
			return nil
		case st != asyncStarted:
			return fmt.Errorf("async operation finished with status %d", st)
		case time.Now().After(deadline):
			return fmt.Errorf("async operation did not finish within %v", asyncWait)
		}
	}
}

func fromDateTime(ticks int64) time.Time {
	const epochDiff = 116444736000000000 // between 1601-01-01 and 1970-01-01 in 100-ns intervals
	return time.Unix(0, (ticks-epochDiff)*100)
}

func newHString(s string) (uintptr, error) {
	u := utf16.Encode([]rune(s))
	var h uintptr
	if hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&h))); !hrOK(hr) {
		return 0, fmt.Errorf("WindowsCreateString: hr=0x%08X", uint32(hr))
	}
	return h, nil
}

func deleteHString(h uintptr) {
	if h != 0 {
		procWindowsDeleteString.Call(h)
	}
}

func hstringToString(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := procWindowsGetStringRawBuffer.Call(h, uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return ""
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n)))
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
