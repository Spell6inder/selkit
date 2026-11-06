package i18n

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// systemLanguages lists the Windows UI languages in order of preference ("uk-UA", "en-US").
func systemLanguages() []string {
	langs, _ := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	return langs
}

var procGetLocaleInfoEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetLocaleInfoEx")

const localeSNativeLanguageName = 0x04

// LanguageName is the name of a language in that language itself ("українська", "English").
func LanguageName(code string) string {
	name, err := windows.UTF16PtrFromString(code)
	if err != nil {
		return code
	}
	var buf [64]uint16
	n, _, _ := procGetLocaleInfoEx.Call(uintptr(unsafe.Pointer(name)), localeSNativeLanguageName,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n <= 1 {
		return code
	}
	return windows.UTF16ToString(buf[:])
}
