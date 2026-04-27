package drm

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// newTestImage creates a test image with a predictable pattern.
// Moved here to avoid import cycle with the main pidisp package.
func newTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 0xff})
		}
	}
	return img
}

func newTestDevice(width, height int) *Device {
	stride := width * 4 // ABGR8888 is always 4 bytes per pixel
	return &Device{
		width:  width,
		height: height,
		stride: stride,
		data:   make([]byte, stride*height),
	}
}

func newTestDevicePaddedStride(width, height, extraBytes int) *Device {
	stride := width*4 + extraBytes
	return &Device{
		width:  width,
		height: height,
		stride: stride,
		data:   make([]byte, stride*height),
	}
}

// TestBlit verifies that Device.Blit copies every pixel from the source image
// unchanged. Rotation is handled in hardware, so Blit is a plain memcopy.
func TestBlit(t *testing.T) {
	img := newTestImage(16, 16)
	d := newTestDevice(16, 16)
	d.Blit(img)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			dstOff := y*d.stride + x*4
			srcOff := y*img.Stride + x*4
			if d.data[dstOff] != img.Pix[srcOff] ||
				d.data[dstOff+1] != img.Pix[srcOff+1] ||
				d.data[dstOff+2] != img.Pix[srcOff+2] ||
				d.data[dstOff+3] != img.Pix[srcOff+3] {
				t.Errorf("pixel (%d,%d): got %02x%02x%02x%02x want %02x%02x%02x%02x",
					x, y,
					d.data[dstOff], d.data[dstOff+1], d.data[dstOff+2], d.data[dstOff+3],
					img.Pix[srcOff], img.Pix[srcOff+1], img.Pix[srcOff+2], img.Pix[srcOff+3])
			}
		}
	}
}

// TestBlitStridePadding verifies that when the device stride is wider than
// width*4 (e.g. for hardware alignment), pixels land at the correct offsets
// and the padding bytes between rows are not written.
func TestBlitStridePadding(t *testing.T) {
	const (
		width   = 4
		height  = 3
		padding = 8 // extra bytes per row
	)
	d := newTestDevicePaddedStride(width, height, padding)

	// Fill with a sentinel so any unexpected write is detectable.
	for i := range d.data {
		d.data[i] = 0xAA
	}

	img := newTestImage(width, height)
	d.Blit(img)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcOff := y*img.Stride + x*4
			dstOff := y*d.stride + x*4
			if !bytes.Equal(d.data[dstOff:dstOff+4], img.Pix[srcOff:srcOff+4]) {
				t.Errorf("pixel (%d,%d): got %x, want %x",
					x, y, d.data[dstOff:dstOff+4], img.Pix[srcOff:srcOff+4])
			}
		}
		// Verify padding bytes between rows are untouched.
		padStart := y*d.stride + width*4
		for i := 0; i < padding; i++ {
			if d.data[padStart+i] != 0xAA {
				t.Errorf("row %d padding byte %d was overwritten (got 0x%02x)", y, i, d.data[padStart+i])
			}
		}
	}
}

// TestBlitFastPath verifies that the fast path (img.Stride == d.stride, single
// copy) produces identical output to the row-by-row slow path.
func TestBlitFastPath(t *testing.T) {
	img := newTestImage(8, 8)
	fast := newTestDevice(8, 8)                  // stride == img.Stride → fast path
	slow := newTestDevicePaddedStride(8, 8, 4)   // stride != img.Stride → slow path

	fast.Blit(img)
	slow.Blit(img)

	// Compare pixel data (excluding padding in slow device).
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			fo := y*fast.stride + x*4
			so := y*slow.stride + x*4
			if !bytes.Equal(fast.data[fo:fo+4], slow.data[so:so+4]) {
				t.Errorf("pixel (%d,%d): fast=%x slow=%x",
					x, y, fast.data[fo:fo+4], slow.data[so:so+4])
			}
		}
	}
}

// TestWidthHeight verifies the Width and Height accessors.
func TestWidthHeight(t *testing.T) {
	d := newTestDevice(123, 45)
	if d.Width() != 123 {
		t.Errorf("Width()=%d, want 123", d.Width())
	}
	if d.Height() != 45 {
		t.Errorf("Height()=%d, want 45", d.Height())
	}
}

func BenchmarkBlit(b *testing.B) {
	d := newTestDevice(800, 480)
	img := newTestImage(800, 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Blit(img)
	}
}