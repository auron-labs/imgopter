package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"imgopter"

	"github.com/egoist/mygo/ui"
)

func newTestApp() *app {
	return &app{quality: 80, format: formats[0], suffix: "-opt"}
}

func tester(a *app) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { a.view(c) }, 920, 600)
}

func TestEmptyState(t *testing.T) {
	a := newTestApp()
	tt := tester(a)
	for _, s := range []string{"Add images…", "Add folder…", "Drop images here", "0 files · 0 B"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
}

func TestFileRowsAndStatusBar(t *testing.T) {
	a := newTestApp()
	a.files = []*fileItem{
		{path: "/in/a.jpg", name: "a.jpg", inSize: 2 << 20, origW: 3000, origH: 2000},
		{path: "/in/b.png", name: "b.png", inSize: 1 << 20, origW: 800, origH: 600,
			status: stDone, outSize: 300 << 10, outW: 800, outH: 600},
		{path: "/in/c.webp", name: "c.webp", inSize: 512, status: stFailed, err: "nope"},
	}
	tt := tester(a)
	for _, s := range []string{"a.jpg", "3000×2000", "pending", "failed", "3 files · 3.0 MB"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
	if !tt.HasText("−71%") { // b.png: 300 KB from 1 MB
		t.Errorf("no savings text in %q", tt.Texts())
	}
}

func TestOptionsMapping(t *testing.T) {
	a := newTestApp()
	a.quality, a.maxW, a.maxH = 65.4, 1200, 0
	a.modeIdx = 1
	a.format = "WebP"
	o := a.options()
	want := imgopter.Options{Quality: 65, MaxWidth: 1200, Mode: imgopter.ModeCrop, Format: imgopter.FormatWebP}
	if o != want {
		t.Fatalf("options %+v, want %+v", o, want)
	}
	// Overwrite keeps the input codec: the format control is disabled and
	// the extension stays the source's.
	a.overwrite = true
	if o := a.options(); o.Format != imgopter.FormatAuto {
		t.Fatalf("overwrite format %v, want auto", o.Format)
	}
}

func TestDestFor(t *testing.T) {
	a := newTestApp()
	f := &fileItem{path: "/pics/pic.jpg", name: "pic.jpg"}
	res := &imgopter.Result{Format: imgopter.FormatWebP}
	if dest, err := a.destFor(f, res); err != nil || dest != "/pics/pic-opt.webp" {
		t.Fatalf("dest %q, err %v", dest, err)
	}
	a.outDir = "/out"
	if dest, err := a.destFor(f, res); err != nil || dest != "/out/pic-opt.webp" {
		t.Fatalf("dest %q, err %v", dest, err)
	}
	// Same path as the source refuses, unless overwriting.
	a.outDir, a.suffix = "/pics", ""
	res.Format = imgopter.FormatJPEG
	if _, err := a.destFor(f, res); err == nil {
		t.Fatal("no error writing over the source")
	}
	a.overwrite = true
	if dest, err := a.destFor(f, res); err != nil || dest != "/pics/pic.jpg" {
		t.Fatalf("overwrite dest %q, err %v", dest, err)
	}
}

func TestRemoveAndClear(t *testing.T) {
	a := newTestApp()
	for _, n := range []string{"x.png", "y.png"} {
		a.files = append(a.files, &fileItem{path: "/in/" + n, name: n, inSize: 10})
		a.have = map[string]bool{"/in/" + n: true}
	}
	a.have["/in/x.png"], a.have["/in/y.png"] = true, true
	tt := tester(a)
	if err := tt.Click("✕"); err != nil {
		t.Fatal(err)
	}
	if len(a.files) != 1 || a.files[0].name != "y.png" {
		t.Fatalf("files after ✕: %v", a.files)
	}
	if a.have["/in/x.png"] {
		t.Fatal("removed path still deduped")
	}
	if err := tt.Click("Clear list"); err != nil {
		t.Fatal(err)
	}
	if len(a.files) != 0 || !tt.HasText("Drop images here") {
		t.Fatalf("files after clear: %v", a.files)
	}
}

func TestLoadDedupesAndThumbs(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := range img.Pix {
		img.Pix[i] = 200 // flat color is fine for a thumbnail
	}
	img.Pix[0] = uint8(color.RGBA{R: 255}.R)
	p := filepath.Join(dir, "shot.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	a := newTestApp()
	a.load([]string{p})
	a.load([]string{p}) // again: deduped
	if len(a.files) != 1 {
		t.Fatalf("files %v", a.files)
	}
	it := a.files[0]
	if it.name != "shot.png" || it.origW != 400 || it.origH != 300 || it.thumb == nil {
		t.Fatalf("item %+v", it)
	}
	if w, h := it.thumb.Size(); w > 96 || h > 96 || w == 0 {
		t.Fatalf("thumb %dx%d", w, h)
	}
}

func TestOptimizeRun(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 200, 150))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	src := filepath.Join(dir, "big.png")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	a := newTestApp()
	a.load([]string{src})
	a.start()
	select {
	case <-a.runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not finish")
	}
	it := a.files[0]
	if it.status != stDone {
		t.Fatalf("status %d, err %q", it.status, it.err)
	}
	if it.outSize <= 0 || it.outW != 200 || it.outH != 150 {
		t.Fatalf("out %+v", it)
	}
	if _, err := os.Stat(filepath.Join(dir, "big-opt.png")); err != nil {
		t.Fatal(err)
	}
	if a.done != 1 || a.lastNote == "" {
		t.Fatalf("done %d, note %q", a.done, a.lastNote)
	}
}
