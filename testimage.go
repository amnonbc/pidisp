package pidisp

import (
	"image"
	"image/color"
)

// NewTestImage returns an RGBA image filled with a non-trivial pattern
// (R=x, G=y, B=x+y, A=255). Used by tests in the drm and fb sub-packages.
func NewTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 0xff})
		}
	}
	return img
}
