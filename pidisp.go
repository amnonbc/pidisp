// Package pidisp opens a display on a Raspberry Pi (or any Linux system) and
// blits image.RGBA frames onto it.
//
// It tries DRM/KMS first (via the [drm] sub-package) and falls back to the
// Linux framebuffer (/dev/fb0) automatically. Callers only see the [Display]
// interface; the backend is chosen at runtime.
//
// # Basic usage
//
//	d, err := pidisp.Open(pidisp.Options{Rotate: true})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer d.Close()
//
//	img := image.NewRGBA(image.Rect(0, 0, d.Width(), d.Height()))
//	// ... draw into img ...
//	d.Blit(img)
package pidisp

import "image"

// Display is a handle to an open hardware display.
// Obtain one via [Open]; release it with [Close].
type Display interface {
	// Width returns the display width in pixels.
	Width() int

	// Height returns the display height in pixels.
	Height() int

	// Blit copies img to the display as fast as possible.
	// img must be at least Width×Height pixels.
	Blit(img *image.RGBA)

	// Close releases all resources held by the display.
	Close()
}
