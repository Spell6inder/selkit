package settings

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"unsafe"
)

var (
	procPrintWindow            = user32.NewProc("PrintWindow")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

// shot saves the window as it is drawn into a PNG file, for SELKIT_SETTINGS_SHOTS.
func shot(hwnd uintptr, path string) error {
	var r rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	dc, _, _ := procCreateCompatibleDC.Call(screen)
	defer procDeleteDC.Call(dc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(screen, uintptr(w), uintptr(h))
	defer procDeleteObject.Call(bmp)
	procSelectObject.Call(dc, bmp)
	procPrintWindow.Call(hwnd, dc, 2) // PW_RENDERFULLCONTENT

	hdr := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	pix := make([]byte, w*h*4)
	procGetDIBits.Call(dc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&hdr)), 0)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(pix); i += 4 { // BGRA → RGBA, opaque
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], 255
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// shotDir is where TestWindowVisual saves each tab, if set.
func shotDir() string { return os.Getenv("SELKIT_SETTINGS_SHOTS") }

func shotPath(i int) string { return filepath.Join(shotDir(), "tab-"+string(rune('0'+i))+".png") }
