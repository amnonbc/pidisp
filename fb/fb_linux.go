// Package fb drives a Linux framebuffer device such as /dev/fb0 using a
// single ioctl and mmap — no external dependencies, no DRM master required.
//
// # When to use this package
//
// fb is the simpler of the two display backends. It works on any Linux system
// with a framebuffer driver and requires no special privileges beyond read/write
// access to /dev/fb0. It is used as a fallback when the [drm] package cannot
// acquire DRM master (e.g. a desktop display server is running, or the kernel
// driver does not support modesetting).
//
// The trade-off is performance: Blit must convert each pixel from Go's RGBA
// layout to the hardware format (RGB565 or XRGB8888), and software rotation
// requires writing pixels in reverse order. On a Raspberry Pi 2 at 800×480
// this costs ~53 ms per frame at 16 bpp. See the [drm] package for a 32×
// faster alternative when DRM/KMS is available.
//
// # Features used
//
//   - FBIOGET_VSCREENINFO ioctl — queries display geometry (width, height,
//     bits-per-pixel) and per-channel bit-field offsets (red, green, blue,
//     transp), which vary between drivers and colour depths.
//   - mmap on the device fd — maps the framebuffer directly into process
//     memory for zero-copy pixel writes.
//   - Bayer 4×4 ordered dithering (16 bpp only) — adds a position-dependent
//     threshold before quantising 8-bit channels to 5/6 bits, spreading
//     quantisation error across a 4×4 tile and reducing colour banding on
//     anti-aliased text.
//   - Software 180° rotation — when enabled at Open time, pixels are written
//     in reverse row and column order.
//
// # References
//
//   - Kernel header: include/uapi/linux/fb.h
//   - Kernel docs: https://www.kernel.org/doc/html/latest/fb/api.html
package fb

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const fbioGetVScreenInfo = 0x4600

// bayer4x4 is a normalised 4×4 Bayer ordered-dither threshold matrix scaled
// to the range [0, 15]. Adding this value to an 8-bit channel before
// right-shifting spreads quantisation error across a 4×4 pixel tile.
var bayer4x4 = [4][4]uint8{
	{0, 8, 2, 10},
	{12, 4, 14, 6},
	{3, 11, 1, 9},
	{15, 7, 13, 5},
}

// clamp8 clamps v to [0, 255].
func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

type fbBitField struct {
	Offset   uint32
	Length   uint32
	MsbRight uint32
}

// fbVarScreenInfo mirrors struct fb_var_screeninfo from <linux/fb.h>.
// All fields are fixed-width types so the layout is the same on 32- and
// 64-bit ARM Linux.
type fbVarScreenInfo struct {
	XRes        uint32
	YRes        uint32
	XResVirtual uint32
	YResVirtual uint32
	XOffset     uint32
	YOffset     uint32
	BitsPerPixel uint32
	Grayscale   uint32
	Red         fbBitField
	Green       fbBitField
	Blue        fbBitField
	Transp      fbBitField
	NonStd      uint32
	Activate    uint32
	Height      uint32
	Width       uint32
	AccelFlags  uint32
	PixClock    uint32
	LeftMargin  uint32
	RightMargin uint32
	UpperMargin uint32
	LowerMargin uint32
	HSyncLen    uint32
	VSyncLen    uint32
	Sync        uint32
	VMode       uint32
	Rotate      uint32
	Colorspace  uint32
	Reserved    [4]uint32
}

// Device represents an open framebuffer device.
type Device struct {
	width  int
	height int
	stride int
	bpp    int
	rotate bool
	vinfo  fbVarScreenInfo
	data   []byte
}

// NewTestImage returns an RGBA image filled with a non-trivial pattern
// (R=x, G=y, B=x+y, A=255). Used by tests in this package.
func NewTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint8(x)
			g := uint8(y)
			b := uint8(x + y)
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 0xff})
		}
	}
	return img
}

// Width returns the display width in pixels.
func (fb *Device) Width() int { return fb.width }

// Height returns the display height in pixels.
func (fb *Device) Height() int { return fb.height }

// Data returns the raw framebuffer bytes. Used by tests to inspect output.
func (fb *Device) Data() []byte { return fb.data }

// NewTestDevice constructs a Device backed by an in-memory buffer for use in
// tests. bpp must be 16 or 32; bit-field offsets are set to the standard
// RGB565 or XRGB8888 layout.
func NewTestDevice(width, height, bpp int, rotate bool) *Device {
	bytesPerPixel := bpp / 8
	stride := width * bytesPerPixel
	vinfo := fbVarScreenInfo{
		XRes:         uint32(width),
		YRes:         uint32(height),
		BitsPerPixel: uint32(bpp),
	}
	// RGB565: R at bit 11 (5 bits), G at bit 5 (6 bits), B at bit 0 (5 bits).
	// RGB8888: R at bit 0, G at bit 8, B at bit 16.
	if bpp == 16 {
		vinfo.Red = fbBitField{Offset: 11}
		vinfo.Green = fbBitField{Offset: 5}
		vinfo.Blue = fbBitField{Offset: 0}
	} else {
		vinfo.Red = fbBitField{Offset: 0}
		vinfo.Green = fbBitField{Offset: 8}
		vinfo.Blue = fbBitField{Offset: 16}
		vinfo.Transp = fbBitField{Offset: 24}
	}
	return &Device{
		width:  width,
		height: height,
		stride: stride,
		bpp:    bpp,
		rotate: rotate,
		vinfo:  vinfo,
		data:   make([]byte, stride*height),
	}
}

// Open opens the framebuffer device at dev, queries its geometry and pixel
// format, and maps the framebuffer into memory. Returns an error if the
// device cannot be opened, the ioctl fails, the mmap fails, or the pixel
// depth is not 16 or 32 bpp.
func Open(dev string, rotate bool) (*Device, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dev, err)
	}

	var vinfo fbVarScreenInfo
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		fbioGetVScreenInfo,
		uintptr(unsafe.Pointer(&vinfo)),
	)
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("ioctl FBIOGET_VSCREENINFO: %w", errno)
	}

	width := int(vinfo.XRes)
	height := int(vinfo.YRes)
	bpp := int(vinfo.BitsPerPixel)
	if bpp != 16 && bpp != 32 {
		f.Close()
		return nil, fmt.Errorf("unsupported framebuffer depth: %d bpp", bpp)
	}

	stride := width * bpp / 8
	if raw, err := os.ReadFile("/sys/class/graphics/fb0/stride"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && n > 0 {
			stride = n
		}
	}

	fbSize := stride * height
	data, err := syscall.Mmap(
		int(f.Fd()),
		0,
		fbSize,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("mmap: %w", err)
	}

	slog.Info("framebuffer", "width", width, "height", height, "bpp", bpp, "stride", stride,
		"red", vinfo.Red.Offset, "green", vinfo.Green.Offset, "blue", vinfo.Blue.Offset)
	return &Device{
		width:  width,
		height: height,
		stride: stride,
		bpp:    bpp,
		rotate: rotate,
		vinfo:  vinfo,
		data:   data,
	}, nil
}

// Close unmaps the framebuffer memory.
func (fb *Device) Close() {
	syscall.Munmap(fb.data)
}

// Blit copies img to the framebuffer, optionally rotating 180 degrees, and
// converting from RGBA to the hardware pixel format described by the vinfo bitfields.
func (fb *Device) Blit(img *image.RGBA) {
	rOff := fb.vinfo.Red.Offset
	gOff := fb.vinfo.Green.Offset
	bOff := fb.vinfo.Blue.Offset
	aOff := fb.vinfo.Transp.Offset
	bytesPerPixel := fb.bpp / 8

	for y := 0; y < fb.height; y++ {
		// Each row in the source image is a slice of 4-byte RGBA pixels.
		srcRow := img.Pix[y*img.Stride:]

		// When rotating 180°, row 0 maps to the last row of the framebuffer,
		// row 1 to the second-to-last, and so on.
		dstY := y
		if fb.rotate {
			dstY = fb.height - 1 - y
		}

		// Walk src forward by 4 bytes per pixel; walk dst forward or backward
		// by bytesPerPixel, avoiding a per-pixel multiply.
		srcOff := 0
		dstOff := dstY * fb.stride
		dstStep := bytesPerPixel
		if fb.rotate {
			dstOff += (fb.width - 1) * bytesPerPixel
			dstStep = -bytesPerPixel
		}

		// Cache the bayer row for this y — it is constant across the x loop.
		bayerRow := bayer4x4[y&3]

		// Pack the pixel into the hardware format. The vinfo bitfields
		// tell us at which bit offset each colour channel sits, which
		// varies between framebuffer drivers and colour depths.
		switch fb.bpp {
		case 32:
			for x := 0; x < fb.width; x++ {
				r := srcRow[srcOff]
				g := srcRow[srcOff+1]
				b := srcRow[srcOff+2]
				a := srcRow[srcOff+3]
				// Shift each 8-bit channel to its hardware bit position and OR together.
				px := uint32(r)<<rOff | uint32(g)<<gOff | uint32(b)<<bOff | uint32(a)<<aOff
				binary.LittleEndian.PutUint32(fb.data[dstOff:], px)
				srcOff += 4
				dstOff += dstStep
			}
		case 16:
			for x := 0; x < fb.width; x++ {
				r := srcRow[srcOff]
				g := srcRow[srcOff+1]
				b := srcRow[srcOff+2]
				// RGB565 with Bayer 4×4 ordered dithering.
				// Without dithering, anti-aliased font edges quantise in
				// steps of 8 (R/B) or 4 (G), producing a visible staircase.
				// Adding a position-dependent threshold before the right-shift
				// spreads the quantisation error across neighbouring pixels.
				d := bayerRow[x&3]
				r5 := uint16(clamp8(int(r)+int(d>>3))) >> 3
				g6 := uint16(clamp8(int(g)+int(d>>2))) >> 2
				b5 := uint16(clamp8(int(b)+int(d>>3))) >> 3
				binary.LittleEndian.PutUint16(fb.data[dstOff:], r5<<uint(rOff)|g6<<uint(gOff)|b5<<uint(bOff))
				srcOff += 4
				dstOff += dstStep
			}
		}
	}
}
