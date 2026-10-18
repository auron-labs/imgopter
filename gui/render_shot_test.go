package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"
	"github.com/egoist/mygo/ui"
)

// TestRenderShot exercises the whole draw path: it renders the populated UI
// to a real frame and drops the PNG alongside the test's temp dir.
func TestRenderShot(t *testing.T) {
	dir := t.TempDir()
	a := &app{quality: 80, format: formats[0], suffix: "-opt", maxW: 1200}
	r := rand.New(rand.NewSource(2))
	for i, name := range []string{"beach.jpg", "screenshot.png", "product.webp"} {
		m := image.NewRGBA(image.Rect(0, 0, 400+100*i, 300+80*i))
		for y := 0; y < m.Bounds().Dy(); y++ {
			for x := 0; x < m.Bounds().Dx(); x++ {
				m.Set(x, y, color.RGBA{
					uint8(x%255) + uint8(r.Intn(20)),
					uint8(math.Sin(float64(y)/50) * 100),
					uint8(y % 255), 255})
			}
		}
		p := filepath.Join(dir, name)
		f, _ := os.Create(p)
		png.Encode(f, m)
		f.Close()
		fi, _ := os.Stat(p)
		it := &fileItem{path: p, name: name, inSize: fi.Size()}
		if img := decodeImage(p); img != nil {
			it.origW, it.origH = img.Bounds().Dx(), img.Bounds().Dy()
			it.thumb = ui.NewBitmap(imaging.Fit(img, 96, 96, imaging.Lanczos))
		}
		a.files = append(a.files, it)
	}
	a.files[0].status = stDone
	a.files[0].outSize = a.files[0].inSize / 4
	a.files[1].status = stWorking
	a.files[2].err = "decode: busted"
	a.files[2].status = stFailed
	a.lastNote = "Optimized 1 of 3 — 1.2 MB → 300.0 KB"

	tt := ui.NewTester(a.view, 920, 600)
	tt.Frame()
	img := tt.Image()
	out, err := os.Create(filepath.Join(t.TempDir(), "frame.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(out, img); err != nil {
		t.Fatal(err)
	}
	out.Close()
}
