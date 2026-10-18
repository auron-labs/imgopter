package imgopter

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{
				uint8(x * 255 / max(1, w)),
				uint8(y * 255 / max(1, h)),
				128, 255,
			})
		}
	}
	return img
}

func noise(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rnd := rand.New(rand.NewSource(42))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{
				uint8(rnd.Intn(256)),
				uint8(rnd.Intn(256)),
				uint8(rnd.Intn(256)),
				255,
			})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseFormat(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want Format
		err  bool
	}{
		{"auto", FormatAuto, false},
		{"jpeg", FormatJPEG, false},
		{"JPG", FormatJPEG, false},
		{"Png", FormatPNG, false},
		{"webp", FormatWebP, false},
		{" gif ", FormatAuto, true},
		{"", FormatAuto, true},
	} {
		got, err := ParseFormat(tt.in)
		if (err != nil) != tt.err {
			t.Fatalf("ParseFormat(%q) err=%v, want err=%v", tt.in, err, tt.err)
		}
		if !tt.err && got != tt.want {
			t.Fatalf("ParseFormat(%q)=%v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatStringExt(t *testing.T) {
	for _, tt := range []struct {
		f   Format
		str string
		ext string
	}{
		{FormatAuto, "auto", ""},
		{FormatJPEG, "jpeg", ".jpg"},
		{FormatPNG, "png", ".png"},
		{FormatWebP, "webp", ".webp"},
	} {
		if tt.f.String() != tt.str || tt.f.Ext() != tt.ext {
			t.Fatalf("Format(%d) = %q/%q, want %q/%q", int(tt.f), tt.f.String(), tt.f.Ext(), tt.str, tt.ext)
		}
	}
}

func TestParseMode(t *testing.T) {
	if m, err := ParseMode("fit"); err != nil || m != ModeFit {
		t.Fatalf("ParseMode(fit)=%v,%v", m, err)
	}
	if m, err := ParseMode("CROP"); err != nil || m != ModeCrop {
		t.Fatalf("ParseMode(CROP)=%v,%v", m, err)
	}
	if _, err := ParseMode("bogus"); err == nil {
		t.Fatal("ParseMode(bogus) expected error")
	}
	if ModeFit.String() != "fit" || ModeCrop.String() != "crop" {
		t.Fatal("Mode.String wrong")
	}
}

func TestNormalized(t *testing.T) {
	for _, q := range []int{-5, 0, 101, 1000} {
		if got := (Options{Quality: q}).normalized().Quality; got != 80 {
			t.Fatalf("Quality %d normalized to %d, want 80", q, got)
		}
	}
	if got := (Options{Quality: 60}).normalized().Quality; got != 60 {
		t.Fatalf("Quality 60 normalized to %d", got)
	}
	o := Options{Quality: 80, MaxWidth: -3, MaxHeight: -1}.normalized()
	if o.MaxWidth != 0 || o.MaxHeight != 0 {
		t.Fatal("negative bounds not clamped to 0")
	}
}

func TestSavings(t *testing.T) {
	if got := (&Result{InBytes: 1000, OutBytes: 250}).Savings(); got != 75 {
		t.Fatalf("Savings=%v, want 75", got)
	}
	if got := (&Result{InBytes: 100, OutBytes: 150}).Savings(); got != -50 {
		t.Fatalf("Savings=%v, want -50", got)
	}
	if got := (&Result{}).Savings(); got != 0 {
		t.Fatalf("Savings=%v, want 0", got)
	}
}

func TestSniffFormat(t *testing.T) {
	pngSrc := encodePNG(t, gradient(8, 8))
	jpgSrc := encodeJPEG(t, gradient(8, 8), 80)
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, gradient(8, 8), nil); err != nil {
		t.Fatal(err)
	}
	if got := SniffFormat(pngSrc); got != FormatPNG {
		t.Fatalf("sniff png = %v", got)
	}
	if got := SniffFormat(jpgSrc); got != FormatJPEG {
		t.Fatalf("sniff jpeg = %v", got)
	}
	if got := SniffFormat(gifBuf.Bytes()); got != FormatAuto {
		t.Fatalf("sniff gif = %v, want FormatAuto", got)
	}
	if got := SniffFormat([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")); got != FormatWebP {
		t.Fatalf("sniff webp magic = %v", got)
	}
	if got := SniffFormat([]byte("garbage")); got != FormatAuto {
		t.Fatalf("sniff garbage = %v", got)
	}
}

func TestJPEGRoundTrip(t *testing.T) {
	src := encodeJPEG(t, noise(320, 240), 95)
	res, err := Optimize(src, Options{Quality: 60, Format: FormatJPEG})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatJPEG {
		t.Fatalf("format=%v, want jpeg", res.Format)
	}
	if res.Width != 320 || res.Height != 240 || res.OrigWidth != 320 || res.OrigHeight != 240 {
		t.Fatalf("dims %dx%d orig %dx%d", res.Width, res.Height, res.OrigWidth, res.OrigHeight)
	}
	if !bytes.HasPrefix(res.Data, []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatal("output missing jpeg magic")
	}
	if res.OutBytes >= res.InBytes {
		t.Fatalf("q60 output %d >= q95 input %d", res.OutBytes, res.InBytes)
	}
}

func TestPNGLosslessKeepsDims(t *testing.T) {
	src := encodePNG(t, gradient(300, 200))
	res, err := Optimize(src, Options{Quality: 100, Format: FormatPNG})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatPNG || res.Width != 300 || res.Height != 200 {
		t.Fatalf("format=%v dims=%dx%d", res.Format, res.Width, res.Height)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 300 || cfg.Height != 200 {
		t.Fatalf("decoded dims %dx%d", cfg.Width, cfg.Height)
	}
}

func TestPNGPalettized(t *testing.T) {
	src := encodePNG(t, noise(256, 256))
	lossy, err := Optimize(src, Options{Quality: 50, Format: FormatPNG})
	if err != nil {
		t.Fatal(err)
	}
	lossless, err := Optimize(src, Options{Quality: 100, Format: FormatPNG})
	if err != nil {
		t.Fatal(err)
	}
	if lossy.OutBytes >= lossless.OutBytes {
		t.Fatalf("palettized %d >= lossless %d", lossy.OutBytes, lossless.OutBytes)
	}
	decoded, err := png.Decode(bytes.NewReader(lossy.Data))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.(*image.Paletted); !ok {
		t.Fatalf("decoded %T, want *image.Paletted", decoded)
	}
}

func TestWebPSmallerOnNoise(t *testing.T) {
	src := encodePNG(t, noise(256, 256))
	res, err := Optimize(src, Options{Quality: 50, Format: FormatWebP})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatWebP {
		t.Fatalf("format=%v", res.Format)
	}
	if res.OutBytes >= res.InBytes {
		t.Fatalf("webp %d >= input %d", res.OutBytes, res.InBytes)
	}
	if len(res.Data) < 12 || !bytes.Equal(res.Data[:4], []byte("RIFF")) || !bytes.Equal(res.Data[8:12], []byte("WEBP")) {
		t.Fatal("output missing webp magic")
	}
	img, err := decodeImage(res.Data, FormatWebP)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("decoded webp dims %v", img.Bounds())
	}
}

func TestFitDims(t *testing.T) {
	for _, tt := range []struct {
		w, h, maxW, maxH int
		wantW, wantH     int
	}{
		{4000, 3000, 1000, 0, 1000, 750},
		{4000, 3000, 0, 750, 1000, 750},
		{4000, 3000, 2000, 2000, 2000, 1500},
		{4000, 3000, 0, 0, 4000, 3000},       // unconstrained
		{4000, 3000, 8000, 8000, 4000, 3000}, // never enlarge
		{4000, 3000, 4000, 3000, 4000, 3000}, // already exact
		{100, 100, 0, 50, 50, 50},
		{999, 667, 500, 0, 500, 334},
		{3, 2, 1, 0, 1, 1},
	} {
		gotW, gotH := fitDims(tt.w, tt.h, tt.maxW, tt.maxH)
		if gotW != tt.wantW || gotH != tt.wantH {
			t.Fatalf("fitDims(%d,%d | %d,%d) = %dx%d, want %dx%d",
				tt.w, tt.h, tt.maxW, tt.maxH, gotW, gotH, tt.wantW, tt.wantH)
		}
	}
}

func TestFitResizeEndToEnd(t *testing.T) {
	src := encodeJPEG(t, gradient(1600, 1200), 90)
	res, err := Optimize(src, Options{Quality: 80, MaxWidth: 400, Mode: ModeFit, Format: FormatJPEG})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 400 || res.Height != 300 {
		t.Fatalf("fit dims %dx%d, want 400x300", res.Width, res.Height)
	}
	// No upscale when already within bounds.
	res2, err := Optimize(src, Options{Quality: 80, MaxWidth: 1600, MaxHeight: 1200, Mode: ModeFit, Format: FormatJPEG})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Width != 1600 || res2.Height != 1200 {
		t.Fatalf("no-op fit dims %dx%d", res2.Width, res2.Height)
	}
}

func TestCropResize(t *testing.T) {
	src := encodeJPEG(t, gradient(1600, 1200), 90)
	res, err := Optimize(src, Options{
		Quality: 80, MaxWidth: 500, MaxHeight: 500, Mode: ModeCrop, Format: FormatJPEG,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 500 || res.Height != 500 {
		t.Fatalf("crop dims %dx%d, want 500x500", res.Width, res.Height)
	}
	// Single bound in crop mode falls back to fit semantics.
	res2, err := Optimize(src, Options{
		Quality: 80, MaxWidth: 400, Mode: ModeCrop, Format: FormatJPEG,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Width != 400 || res2.Height != 300 {
		t.Fatalf("one-bound crop dims %dx%d, want 400x300", res2.Width, res2.Height)
	}
}

func TestJPEGToWebPConversion(t *testing.T) {
	src := encodeJPEG(t, noise(200, 150), 90)
	res, err := Optimize(src, Options{Quality: 70, Format: FormatWebP})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatWebP {
		t.Fatalf("format=%v", res.Format)
	}
	if !bytes.Equal(res.Data[:4], []byte("RIFF")) || !bytes.Equal(res.Data[8:12], []byte("WEBP")) {
		t.Fatal("output not webp")
	}
}

func TestAutoFormatKeepsJPEG(t *testing.T) {
	src := encodeJPEG(t, gradient(64, 64), 85)
	res, err := Optimize(src, Options{Quality: 80, Format: FormatAuto})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatJPEG {
		t.Fatalf("auto on jpeg resolved to %v", res.Format)
	}
}

func TestAutoFormatGIFToPNG(t *testing.T) {
	var buf bytes.Buffer
	if err := gif.Encode(&buf, gradient(100, 80), nil); err != nil {
		t.Fatal(err)
	}
	res, err := Optimize(buf.Bytes(), Options{Quality: 100, Format: FormatAuto})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatPNG {
		t.Fatalf("auto on gif resolved to %v, want png", res.Format)
	}
	if !bytes.HasPrefix(res.Data, []byte("\x89PNG")) {
		t.Fatal("output missing png magic")
	}
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("dims %dx%d", res.Width, res.Height)
	}
}

func TestJPEGFlattensAlpha(t *testing.T) {
	// Transparent image through JPEG must not come out black in transparent
	// areas: white background should dominate the center pixel.
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 0}) // fully transparent red
		}
	}
	src := encodePNG(t, img)
	res, err := Optimize(src, Options{Quality: 95, Format: FormatJPEG})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(16, 16).RGBA()
	if r>>8 < 200 || g>>8 < 200 || b>>8 < 200 {
		t.Fatalf("transparent area decoded dark (r=%d g=%d b=%d), want ~white", r>>8, g>>8, b>>8)
	}
}

func TestOptimizeFile(t *testing.T) {
	dir := t.TempDir()
	src := encodePNG(t, gradient(120, 90))
	path := filepath.Join(dir, "in.png")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := OptimizeFile(path, Options{Quality: 100, Format: FormatAuto})
	if err != nil {
		t.Fatal(err)
	}
	if res.InBytes != len(src) {
		t.Fatalf("InBytes=%d, want %d", res.InBytes, len(src))
	}
	if res.Format != FormatPNG || res.Width != 120 || res.Height != 90 {
		t.Fatalf("format=%v dims=%dx%d", res.Format, res.Width, res.Height)
	}
}

func TestBadInput(t *testing.T) {
	if _, err := Optimize(nil, Options{Quality: 80}); err == nil {
		t.Fatal("empty input expected error")
	}
	if _, err := Optimize([]byte("definitely not an image"), Options{Quality: 80}); err == nil {
		t.Fatal("garbage input expected error")
	}
}

func TestIsSupportedInput(t *testing.T) {
	for in, want := range map[string]bool{
		"a.jpg": true, "b.JPEG": true, "c.png": true, "d.webp": true,
		"e.gif": true, "f.bmp": true, "g.tif": true, "h.TIFF": true,
		"i.txt": false, "j": false, "k.pdf": false,
	} {
		if got := IsSupportedInput(in); got != want {
			t.Fatalf("IsSupportedInput(%q)=%v, want %v", in, got, want)
		}
	}
}

// orientation.webp is a lossless 2x3 color grid with EXIF orientation 6 (90° CW).
func TestWebPOrientationAppliedBeforeStrippingMetadata(t *testing.T) {
	src, err := os.ReadFile("testdata/orientation.webp")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Optimize(src, Options{Quality: 100, Format: FormatPNG})
	if err != nil {
		t.Fatal(err)
	}
	if res.OrigWidth != 3 || res.OrigHeight != 2 || res.Width != 3 || res.Height != 2 {
		t.Fatalf("incorrect oriented dimensions: %+v", res)
	}
	decoded, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	// The original bottom-left pixel moves to the output's top-left.
	plain, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	want := color.NRGBAModel.Convert(plain.At(0, 2)).(color.NRGBA)
	if got != want {
		t.Fatalf("top-left pixel %v", got)
	}
}
