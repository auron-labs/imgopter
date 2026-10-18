package imgopter

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"net/http"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/webp"
	_ "golang.org/x/image/bmp"  // register BMP decoder
	_ "golang.org/x/image/tiff" // register TIFF decoder
)

// SniffFormat detects the encoded format of src from its magic bytes. It only
// reports formats imgopter can also encode (jpeg/png/webp); every other input,
// decodable or not, reports FormatAuto.
func SniffFormat(src []byte) Format {
	if len(src) >= 12 && bytes.Equal(src[:4], []byte("RIFF")) && bytes.Equal(src[8:12], []byte("WEBP")) {
		return FormatWebP
	}
	switch http.DetectContentType(src) {
	case "image/jpeg":
		return FormatJPEG
	case "image/png":
		return FormatPNG
	case "image/webp":
		return FormatWebP
	}
	return FormatAuto
}

func decodeImage(src []byte, sniffed Format) (image.Image, error) {
	// image.Decode doesn't know WebP, so handle it before the generic path.
	// Everything else goes through imaging for EXIF auto-orientation.
	if sniffed == FormatWebP {
		img, err := webp.Decode(bytes.NewReader(src), webp.Options{AutoRotate: true})
		if err != nil {
			return nil, fmt.Errorf("imgopter: decode webp: %w", err)
		}
		return img, nil
	}
	img, err := imaging.Decode(bytes.NewReader(src), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("imgopter: decode: %w", err)
	}
	return img, nil
}
