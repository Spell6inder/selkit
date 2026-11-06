// Package build turns the SVG sources in resources/icon into the ICO files the program embeds.
// It runs at generate time only (see internal/icon/winres), so the exe carries no SVG renderer.
package build

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// Sizes are the frames packed into each ICO: tray at 100–200% scaling, Explorer and the large view.
var Sizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

// Up to this size frames are drawn from the simplified 16 px source.
const smallMax = 24

// Output file names in resources/icon.
const (
	ActiveICO   = "selkit.ico"
	DisabledICO = "selkit-off.ico"
)

// Generate renders every frame from the SVG sources in dir and returns the ICO files by name.
func Generate(dir string) (map[string][]byte, error) {
	frames, err := Frames(dir)
	if err != nil {
		return nil, err
	}
	gray := make([]*image.NRGBA, len(frames))
	for i, f := range frames {
		gray[i] = Desaturate(f)
	}
	return map[string][]byte{ActiveICO: ICO(frames), DisabledICO: ICO(gray)}, nil
}

// Frames renders the mark at every size in Sizes.
func Frames(dir string) ([]*image.NRGBA, error) {
	var out []*image.NRGBA
	for _, n := range Sizes {
		src := "selkit.svg"
		if n <= smallMax {
			src = "selkit-16.svg"
		}
		img, err := Render(filepath.Join(dir, src), n)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, nil
}

// Render rasterizes an SVG file into an n×n image.
func Render(path string, n int) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	icon, err := oksvg.ReadIconStream(f, oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(n), float64(n))
	rgba := image.NewRGBA(image.Rect(0, 0, n, n))
	scanner := rasterx.NewScannerGV(n, n, rgba, rgba.Bounds())
	icon.Draw(rasterx.NewDasher(n, n, scanner), 1)

	// ICO frames use straight alpha, the rasterizer produces premultiplied.
	out := image.NewNRGBA(rgba.Bounds())
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			out.Set(x, y, color.NRGBAModel.Convert(rgba.At(x, y)))
		}
	}
	return out, nil
}

// Desaturate makes the "disabled" variant: luminance only, slightly lightened so it reads as inactive.
func Desaturate(img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2])
		l := 0.299*r + 0.587*g + 0.114*b
		l = l + (255-l)*0.25
		v := uint8(l + 0.5)
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = v, v, v, img.Pix[i+3]
	}
	return out
}

// ICO packs frames into an ICO file: 32bpp BMP up to 64 px, PNG for larger frames to keep the file small.
func ICO(frames []*image.NRGBA) []byte {
	data := make([][]byte, len(frames))
	for i, f := range frames {
		if f.Bounds().Dx() > 64 {
			var b bytes.Buffer
			png.Encode(&b, f)
			data[i] = b.Bytes()
		} else {
			data[i] = bmpFrame(f)
		}
	}

	var buf bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&buf, le, struct{ Reserved, Type, Count uint16 }{0, 1, uint16(len(frames))})
	offset := 6 + 16*len(frames)
	for i, f := range frames {
		dim := uint8(f.Bounds().Dx()) // 256 is written as 0
		binary.Write(&buf, le, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(data[i])), uint32(offset)})
		offset += len(data[i])
	}
	for _, d := range data {
		buf.Write(d)
	}
	return buf.Bytes()
}

// bmpFrame is the BITMAPINFOHEADER + BGRA pixels (bottom to top) + AND mask that an ICO frame expects.
func bmpFrame(img *image.NRGBA) []byte {
	n := img.Bounds().Dx()
	maskRow := (n + 31) / 32 * 4
	var buf bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&buf, le, struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}{40, int32(n), int32(2 * n), 1, 32, 0, uint32(n * n * 4), 0, 0, 0, 0})
	for y := n - 1; y >= 0; y-- {
		row := img.Pix[y*img.Stride : y*img.Stride+n*4]
		for x := 0; x < n*4; x += 4 {
			buf.Write([]byte{row[x+2], row[x+1], row[x], row[x+3]})
		}
	}
	buf.Write(make([]byte, maskRow*n)) // AND mask: transparency is already in the alpha channel
	return buf.Bytes()
}
