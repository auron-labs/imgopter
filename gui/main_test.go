package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"imgopter"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		{
			path: "/in/b.png", name: "b.png", inSize: 1 << 20, origW: 800, origH: 600,
			status: stDone, outSize: 300 << 10, outW: 800, outH: 600,
		},
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
	path := "/pics/pic.jpg"
	if dest := a.outputSettings().destFor(path, imgopter.FormatWebP); dest != "/pics/pic-opt.webp" {
		t.Fatalf("dest %q", dest)
	}
	a.outDir = "/out"
	if dest := a.outputSettings().destFor(path, imgopter.FormatWebP); dest != "/out/pic-opt.webp" {
		t.Fatalf("dest %q", dest)
	}
	a.overwrite = true
	if dest := a.outputSettings().destFor(path, imgopter.FormatJPEG); dest != path {
		t.Fatalf("overwrite dest %q", dest)
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
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

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
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

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

func waitForRun(t *testing.T, a *app) {
	t.Helper()
	select {
	case <-a.runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not finish")
	}
}

func TestRunSnapshotsOutputSettings(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "original.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 200, 150))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	a.format = "JPEG"
	a.load([]string{src})
	a.start()
	// These edits must affect only the next run, even if the worker has not begun.
	a.overwrite = true
	a.outDir = filepath.Join(dir, "other")
	a.suffix = "-changed"
	waitForRun(t, a)
	if a.files[0].status != stDone {
		t.Fatal(a.files[0].err)
	}
	original, err := os.ReadFile(src)
	if err != nil || !bytes.Equal(original, b.Bytes()) {
		t.Fatalf("source changed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "original-opt.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if imgopter.SniffFormat(data) != imgopter.FormatJPEG {
		t.Fatal("incorrect output format")
	}
	if _, err := os.Stat(a.outDir); !os.IsNotExist(err) {
		t.Fatalf("live output directory used: %v", err)
	}
}

func TestRunRejectsSourceCollisionBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(dir, "a.png"), filepath.Join(dir, "a-opt.png")}
	for _, p := range paths {
		if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp()
	a.load(paths)
	a.start()
	waitForRun(t, a)
	for _, f := range a.files {
		if f.status != stFailed {
			t.Fatalf("status %d", f.status)
		}
		got, err := os.ReadFile(f.path)
		if err != nil || !bytes.Equal(got, b.Bytes()) {
			t.Fatalf("source changed: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "a-opt-opt.png")); !os.IsNotExist(err) {
		t.Fatalf("partial output written: %v", err)
	}
}

func TestWebPThumbnailOrientation(t *testing.T) {
	img := decodeImage("../testdata/orientation.webp")
	if img == nil {
		t.Fatal("decode failed")
	}
	if img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
		t.Fatalf("dimensions %v", img.Bounds())
	}
	src, err := os.ReadFile("../testdata/orientation.webp")
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA)
	want := color.NRGBAModel.Convert(plain.At(0, 2)).(color.NRGBA)
	if got != want {
		t.Fatalf("top-left pixel %v", got)
	}
}
