// pidisp_linux.go provides the Linux implementation of Open.
// It tries DRM/KMS on /dev/dri/card0..card2 and falls back to /dev/fb0.

package pidisp

import (
	"fmt"
	"log/slog"

	"github.com/amnonbc/pidisp/drm"
	"github.com/amnonbc/pidisp/fb"
)

// Options controls how the display is opened.
type Options struct {
	// Rotate requests 180° rotation of the output.
	// On DRM/KMS devices this is offloaded to hardware; on fbdev it is done
	// in software.
	Rotate bool

	// ForceFB skips DRM detection and opens /dev/fb0 directly. Useful for
	// testing the fbdev path on a machine that supports both.
	ForceFB bool

	// Debug logs DRM device information (plane formats, connector details)
	// before opening the display.
	Debug bool
}

// Open opens the best available display device and returns a [Display].
//
// It tries /dev/dri/card0, card1, and card2 in order (unless opts.ForceFB is
// set), using the first DRM device that supports modesetting. If DRM is
// unavailable it opens /dev/fb0 instead.
func Open(opts Options) (Display, error) {
	if opts.Debug {
		for _, card := range []string{"/dev/dri/card0", "/dev/dri/card1", "/dev/dri/card2"} {
			drm.LogPlaneFormats(card)
		}
	}

	if !opts.ForceFB {
		for _, card := range []string{"/dev/dri/card0", "/dev/dri/card1", "/dev/dri/card2"} {
			d, err := drm.Open(card, opts.Rotate)
			if err == nil {
				return d, nil
			}
			slog.Info("DRM unavailable", "card", card, "err", err)
		}
	}

	f, err := fb.Open("/dev/fb0", opts.Rotate)
	if err != nil {
		return nil, fmt.Errorf("framebuffer: %w", err)
	}
	return f, nil
}
