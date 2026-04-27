//go:build !linux

package pidisp

import "errors"

// Options controls how the display is opened.
type Options struct {
	Rotate  bool
	ForceFB bool
	Debug   bool
}

// Open is not supported on non-Linux platforms.
func Open(_ Options) (Display, error) {
	return nil, errors.New("pidisp: not supported on this platform")
}
