package build

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

var iconDir = filepath.Join("..", "..", "..", "resources", "icon")

// TestUpToDate: the committed ICO files must match the SVG sources.
func TestUpToDate(t *testing.T) {
	files, err := Generate(iconDir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(iconDir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("resources/icon/%s is out of date - run: go generate ./cmd/selkit", name)
		}
	}
}

// TestPreview writes var/icon-preview.png: every size magnified on light and dark taskbars,
// with the disabled variant under the tray sizes. Run with SELKIT_ICON_PREVIEW=1.
func TestPreview(t *testing.T) {
	if os.Getenv("SELKIT_ICON_PREVIEW") == "" {
		t.Skip("set SELKIT_ICON_PREVIEW=1")
	}
	frames, err := Frames(iconDir)
	if err != nil {
		t.Fatal(err)
	}
	const zoom, gap = 4, 16
	w := gap
	for _, f := range frames[:len(frames)-1] {
		w += f.Bounds().Dx()*zoom + gap
	}
	rowH := 64*zoom + 2*gap
	img := image.NewRGBA(image.Rect(0, 0, w+256+gap, 2*rowH))
	for row, bg := range []color.RGBA{{0xF3, 0xF3, 0xF3, 255}, {0x20, 0x20, 0x20, 255}} {
		y0 := row * rowH
		draw.Draw(img, image.Rect(0, y0, img.Bounds().Dx(), y0+rowH), image.NewUniform(bg), image.Point{}, draw.Src)
		x := gap
		for _, f := range frames[:len(frames)-1] {
			n := f.Bounds().Dx()
			blit(img, f, x, y0+gap, zoom)
			if n <= 32 {
				blit(img, Desaturate(f), x, y0+gap+34*zoom, zoom)
			}
			x += n*zoom + gap
		}
		blit(img, frames[len(frames)-1], img.Bounds().Dx()-256-gap, y0+gap, 1)
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	if err := os.WriteFile(filepath.Join("..", "..", "..", "var", "icon-preview.png"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// blit draws src onto dst at (x0, y0), magnified zoom times without smoothing.
func blit(dst *image.RGBA, src *image.NRGBA, x0, y0, zoom int) {
	n := src.Bounds().Dx()
	r := image.Rect(0, 0, n*zoom, n*zoom)
	big := image.NewNRGBA(r)
	for y := 0; y < n*zoom; y++ {
		for x := 0; x < n*zoom; x++ {
			big.Set(x, y, src.At(x/zoom, y/zoom))
		}
	}
	draw.Draw(dst, r.Add(image.Pt(x0, y0)), big, image.Point{}, draw.Over)
}
