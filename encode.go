package imgopter

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	"github.com/ericpauley/go-quantize/quantize"
	"github.com/gen2brain/webp"
)

func encodeImage(w io.Writer, img image.Image, f Format, quality int) error {
	switch f {
	case FormatJPEG:
		// JPEG has no alpha; flatten onto opaque white first or transparent
		// regions come out black.
		return jpeg.Encode(w, flatten(img), &jpeg.Options{Quality: quality})
	case FormatPNG:
		if quality < 100 {
			img = palettize(img, quality)
		}
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		return enc.Encode(w, img)
	case FormatWebP:
		return webp.Encode(w, img, webp.Options{Quality: quality, Method: 4})
	}
	return fmt.Errorf("imgopter: no encoder for format %d", int(f))
}

// flatten composites img over an opaque white background.
func flatten(img image.Image) image.Image {
	if o, ok := img.(interface{ Opaque() bool }); ok && o.Opaque() {
		return img
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, image.White, b.Min, draw.Src)
	draw.Draw(dst, b, img, b.Min, draw.Over)
	return dst
}

// palettize quantizes img to a median-cut palette sized by quality
// (q/100 of 256 colors, clamped to 2..256) for smaller PNG output.
func palettize(img image.Image, quality int) *image.Paletted {
	colors := min(256, max(2, int(math.Round(float64(quality)/100*256))))
	palette := quantize.MedianCutQuantizer{
		Aggregation:    quantize.Mean,
		AddTransparent: true,
	}.Quantize(make(color.Palette, 0, colors), img)
	b := img.Bounds()
	dst := image.NewPaletted(b, palette)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}
