package imgopter

import (
	"image"
	"math"

	"github.com/disintegration/imaging"
)

func resizeImage(img image.Image, o Options) image.Image {
	// Crop needs both bounds to cover; a single bound degrades to fit.
	if o.Mode == ModeCrop && o.MaxWidth > 0 && o.MaxHeight > 0 {
		if img.Bounds().Dx() == o.MaxWidth && img.Bounds().Dy() == o.MaxHeight {
			return img
		}
		return imaging.Fill(img, o.MaxWidth, o.MaxHeight, imaging.Center, imaging.Lanczos)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	nw, nh := fitDims(w, h, o.MaxWidth, o.MaxHeight)
	if nw == w && nh == h {
		return img
	}
	return imaging.Resize(img, nw, nh, imaging.Lanczos)
}

// fitDims computes contain-fit target dims for a w×h source. It only ever
// shrinks: an image already inside the bounds keeps its size. A zero bound is
// unconstrained.
func fitDims(w, h, maxW, maxH int) (int, int) {
	scale := math.Inf(1)
	if maxW > 0 {
		scale = math.Min(scale, float64(maxW)/float64(w))
	}
	if maxH > 0 {
		scale = math.Min(scale, float64(maxH)/float64(h))
	}
	if scale >= 1 {
		return w, h
	}
	return max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale)))
}
