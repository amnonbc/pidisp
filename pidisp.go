// Package pidisp opens a display on a Raspberry Pi (or any Linux system) and
// renders image.RGBA frames onto it using double buffering.
//
// It tries DRM/KMS first (via the [drm] sub-package) and falls back to the
// Linux framebuffer (/dev/fb0) automatically. Callers only see the [Display]
// interface; the backend is chosen at runtime.
//
// # Basic usage (zero-copy)
//
//	d, err := pidisp.Open(pidisp.Options{Rotate: true})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer d.Close()
//
//	buf := d.BackBuffer()
//	// ... draw into buf ...
//	d.Flip()
package pidisp

import "image"

// Display is a handle to an open hardware display.
// Obtain one via [Open]; release it with [Close].
type Display interface {
	// Width returns the display width in pixels.
	Width() int

	// Height returns the display height in pixels.
	Height() int

	// BackBuffer returns the non-active buffer for rendering.
	// The returned buffer may be drawn into directly.
	// Call Flip to make the back buffer active (displayed).
	BackBuffer() *image.RGBA

	// Flip makes the back buffer the active (displayed) buffer
	// and prepares a fresh back buffer for the next frame.
	// Returns an error if the flip operation fails.
	Flip() error

	// Blit copies img to the display. Deprecated: use BackBuffer and Flip instead.
	Blit(img *image.RGBA)

	// Close releases all resources held by the display.
	Close()
}
