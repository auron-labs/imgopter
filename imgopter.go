// Package imgopter decodes, resizes and re-encodes images for size
// optimization. Animated inputs are flattened to their first frame and all
// metadata (EXIF, ICC profiles, comments) is stripped on re-encode.
package imgopter

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Format identifies an output image codec.
type Format int

const (
	FormatAuto Format = iota // keep input format
	FormatJPEG               // JPEG output
	FormatPNG                // PNG output
	FormatWebP               // WebP output
)

// ParseFormat parses "auto", "jpeg"/"jpg", "png" or "webp" (case-insensitive).
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return FormatAuto, nil
	case "jpeg", "jpg":
		return FormatJPEG, nil
	case "png":
		return FormatPNG, nil
	case "webp":
		return FormatWebP, nil
	}
	return FormatAuto, fmt.Errorf("imgopter: unknown format %q (want auto|jpeg|png|webp)", s)
}

func (f Format) String() string {
	switch f {
	case FormatJPEG:
		return "jpeg"
	case FormatPNG:
		return "png"
	case FormatWebP:
		return "webp"
	}
	return "auto"
}

// Ext returns the canonical file extension for f, or "" for FormatAuto.
func (f Format) Ext() string {
	switch f {
	case FormatJPEG:
		return ".jpg"
	case FormatPNG:
		return ".png"
	case FormatWebP:
		return ".webp"
	}
	return ""
}

// Mode selects how an image is fitted into MaxWidth/MaxHeight bounds.
type Mode int

const (
	ModeFit  Mode = iota // contain: scale to fit inside MaxWidth×MaxHeight, preserve aspect
	ModeCrop             // cover: scale to cover the box, then center-crop to exact dims
)

// ParseMode parses "fit" or "crop" (case-insensitive).
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fit":
		return ModeFit, nil
	case "crop":
		return ModeCrop, nil
	}
	return ModeFit, fmt.Errorf("imgopter: unknown mode %q (want fit|crop)", s)
}

func (m Mode) String() string {
	if m == ModeCrop {
		return "crop"
	}
	return "fit"
}

// Options controls an Optimize run.
type Options struct {
	Quality   int    // 1–100; <=0 or >100 is clamped to 80. PNG: 100 = lossless, <100 = palette-quantized
	MaxWidth  int    // px bound, 0 = unconstrained
	MaxHeight int    // px bound, 0 = unconstrained
	Mode      Mode   // fit or crop semantics for the bounds
	Format    Format // output format; FormatAuto keeps the input format
}

func (o Options) normalized() Options {
	if o.Quality <= 0 || o.Quality > 100 {
		o.Quality = 80
	}
	if o.MaxWidth < 0 {
		o.MaxWidth = 0
	}
	if o.MaxHeight < 0 {
		o.MaxHeight = 0
	}
	return o
}

// Result describes one optimized image.
type Result struct {
	Data       []byte // encoded output
	Format     Format // resolved output format
	Width      int
	Height     int
	OrigWidth  int
	OrigHeight int
	InBytes    int
	OutBytes   int
}

// Savings returns the percent of bytes saved, 0..100. Negative when the
// output grew.
func (r *Result) Savings() float64 {
	if r.InBytes <= 0 {
		return 0
	}
	return (1 - float64(r.OutBytes)/float64(r.InBytes)) * 100
}

var inputExts = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tif", ".tiff", ".webp"}

// SupportedInputExt lists the decodable input extensions, lowercase with dot.
func SupportedInputExt() []string { return slices.Clone(inputExts) }

// IsSupportedInput reports whether path's extension is a decodable image type.
func IsSupportedInput(path string) bool {
	return slices.Contains(inputExts, strings.ToLower(filepath.Ext(path)))
}

// Optimize decodes src, resizes it per o and re-encodes it. With FormatAuto
// the output keeps the input codec; inputs without an output encoder
// (gif/bmp/tiff) fall back to PNG.
func Optimize(src []byte, o Options) (*Result, error) {
	if len(src) == 0 {
		return nil, errors.New("imgopter: empty input")
	}
	o = o.normalized()

	sniffed := SniffFormat(src)
	img, err := decodeImage(src, sniffed)
	if err != nil {
		return nil, err
	}
	origW, origH := img.Bounds().Dx(), img.Bounds().Dy()

	img = resizeImage(img, o)

	format := o.Format
	if format == FormatAuto {
		format = sniffed
	}
	if format != FormatJPEG && format != FormatPNG && format != FormatWebP {
		format = FormatPNG
	}

	var buf bytes.Buffer
	if err := encodeImage(&buf, img, format, o.Quality); err != nil {
		return nil, fmt.Errorf("imgopter: encode %s: %w", format, err)
	}

	return &Result{
		Data:       buf.Bytes(),
		Format:     format,
		Width:      img.Bounds().Dx(),
		Height:     img.Bounds().Dy(),
		OrigWidth:  origW,
		OrigHeight: origH,
		InBytes:    len(src),
		OutBytes:   buf.Len(),
	}, nil
}

// OptimizeFile reads srcPath and optimizes it. See Optimize.
func OptimizeFile(srcPath string, o Options) (*Result, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, err
	}
	return Optimize(data, o)
}
