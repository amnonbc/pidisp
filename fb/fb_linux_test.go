package fb

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// TestBlit32ChannelOrder verifies that each RGBA channel lands at the correct
// byte position in the 32 bpp output: R=byte0, G=byte1, B=byte2, A=byte3.
func TestBlit32ChannelOrder(t *testing.T) {
	tests := []struct {
		name string
		px   color.RGBA
		want [4]byte
	}{
		{"red", color.RGBA{R: 0xFF}, [4]byte{0xFF, 0x00, 0x00, 0x00}},
		{"green", color.RGBA{G: 0xFF}, [4]byte{0x00, 0xFF, 0x00, 0x00}},
		{"blue", color.RGBA{B: 0xFF}, [4]byte{0x00, 0x00, 0xFF, 0x00}},
		{"alpha", color.RGBA{A: 0xFF}, [4]byte{0x00, 0x00, 0x00, 0xFF}},
		{"all", color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0x44}, [4]byte{0x11, 0x22, 0x33, 0x44}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewTestDevice(1, 1, 32, false)
			img := image.NewRGBA(image.Rect(0, 0, 1, 1))
			img.SetRGBA(0, 0, tc.px)
			d.Blit(img)
			if !bytes.Equal(d.Data(), tc.want[:]) {
				t.Errorf("got %x, want %x", d.Data(), tc.want)
			}
		})
	}
}

// TestBlit16ChannelIsolation verifies each RGB channel contributes to the
// correct bit range in the RGB565 output. Tests use position (0,0) where the
// Bayer threshold is 0, so no dithering affects the result.
//
// RGB565 layout: RRRRRGGGGGGBBBBB — R at bits 15-11, G at bits 10-5, B at bits 4-0.
func TestBlit16ChannelIsolation(t *testing.T) {
	tests := []struct {
		name string
		px   color.RGBA
		want uint16
	}{
		// r=248 → r5=31 at bit 11 → 0xF800
		{"red max", color.RGBA{R: 0xF8}, 0xF800},
		// g=252 → g6=63 at bit 5 → 0x07E0
		{"green max", color.RGBA{G: 0xFC}, 0x07E0},
		// b=248 → b5=31 at bit 0 → 0x001F
		{"blue max", color.RGBA{B: 0xF8}, 0x001F},
		{"all max", color.RGBA{R: 0xF8, G: 0xFC, B: 0xF8}, 0xFFFF},
		{"zero", color.RGBA{}, 0x0000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewTestDevice(1, 1, 16, false)
			img := image.NewRGBA(image.Rect(0, 0, 1, 1))
			img.SetRGBA(0, 0, tc.px)
			d.Blit(img)
			got := binary.LittleEndian.Uint16(d.Data())
			if got != tc.want {
				t.Errorf("pixel %v: got 0x%04x, want 0x%04x", tc.px, got, tc.want)
			}
		})
	}
}

// TestBlit32Rotation verifies that 180° rotation maps src(x,y) to
// dst(width-1-x, height-1-y). Uses a 2x2 grid with a distinct R value per
// pixel so the mapping is unambiguous.
func TestBlit32Rotation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 1, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 2, A: 255})
	img.SetRGBA(0, 1, color.RGBA{R: 3, A: 255})
	img.SetRGBA(1, 1, color.RGBA{R: 4, A: 255})

	d := NewTestDevice(2, 2, 32, true)
	d.Blit(img)

	// 180° rotation: src(x,y) → dst(1-x, 1-y)
	// src(0,0)=R:1 → dst(1,1); src(1,0)=R:2 → dst(0,1)
	// src(0,1)=R:3 → dst(1,0); src(1,1)=R:4 → dst(0,0)
	cases := []struct {
		x, y  int
		wantR byte
	}{
		{0, 0, 4}, {1, 0, 3},
		{0, 1, 2}, {1, 1, 1},
	}
	data := d.Data()
	for _, c := range cases {
		off := c.y*d.Width()*4 + c.x*4
		if data[off] != c.wantR {
			t.Errorf("dst(%d,%d): got R=%d, want R=%d", c.x, c.y, data[off], c.wantR)
		}
	}
}

// TestBlit16Rotation verifies 180° rotation for 16 bpp. Uses a 1-row image so
// rotation reduces to reversing the column order.
func TestBlit16Rotation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	// Max red (0xF800) and max blue (0x001F) are unambiguously distinct in RGB565.
	img.SetRGBA(0, 0, color.RGBA{R: 0xF8, A: 255})
	img.SetRGBA(1, 0, color.RGBA{B: 0xF8, A: 255})

	d := NewTestDevice(2, 1, 16, true)
	d.Blit(img)

	data := d.Data()
	got0 := binary.LittleEndian.Uint16(data[0:])
	got1 := binary.LittleEndian.Uint16(data[2:])
	// After rotation column 0 and 1 swap.
	if got0 != 0x001F || got1 != 0xF800 {
		t.Errorf("got [0x%04x, 0x%04x], want [0x001F, 0xF800]", got0, got1)
	}
}

// TestBlit16Dithering verifies that the Bayer matrix shifts pixel values across
// the quantisation boundary. bayer4x4[0][0]=0 (no boost) and bayer4x4[0][1]=8
// (d>>3=1, adds 1 before the right-shift). A source value of r=7 is just below
// the RGB5 threshold: without dither r5=0, with dither r5=1.
func TestBlit16Dithering(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 7, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 7, A: 255})

	d := NewTestDevice(2, 1, 16, false)
	d.Blit(img)

	data := d.Data()
	r5at0 := binary.LittleEndian.Uint16(data[0:]) >> 11
	r5at1 := binary.LittleEndian.Uint16(data[2:]) >> 11
	if r5at0 != 0 {
		t.Errorf("x=0 (bayer=0): r5=%d, want 0 (no dither)", r5at0)
	}
	if r5at1 != 1 {
		t.Errorf("x=1 (bayer=8): r5=%d, want 1 (dithered up)", r5at1)
	}
}

// TestBlit16DitherClamp verifies that adding the Bayer threshold to a channel
// at maximum value (255) saturates at 255 rather than wrapping around. The
// dithered value must still encode to the maximum quantised output.
func TestBlit16DitherClamp(t *testing.T) {
	// Use a 4-pixel row so all four Bayer values for row 0 are exercised.
	// bayer4x4[0] = {0, 8, 2, 10} — all non-zero except position 0.
	img := image.NewRGBA(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		img.SetRGBA(x, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	}

	d := NewTestDevice(4, 1, 16, false)
	d.Blit(img)

	data := d.Data()
	for x := 0; x < 4; x++ {
		p := binary.LittleEndian.Uint16(data[x*2:])
		if p != 0xFFFF {
			t.Errorf("x=%d: got 0x%04x, want 0xffff (clamped to max)", x, p)
		}
	}
}

// TestWidthHeight verifies the Width and Height accessors.
func TestWidthHeight(t *testing.T) {
	d := NewTestDevice(123, 45, 32, false)
	if d.Width() != 123 {
		t.Errorf("Width()=%d, want 123", d.Width())
	}
	if d.Height() != 45 {
		t.Errorf("Height()=%d, want 45", d.Height())
	}
}

func BenchmarkBlit16(b *testing.B) {
	d := NewTestDevice(800, 480, 16, false)
	img := NewTestImage(800, 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Blit(img)
	}
}

func BenchmarkBlit16Rotate(b *testing.B) {
	d := NewTestDevice(800, 480, 16, true)
	img := NewTestImage(800, 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Blit(img)
	}
}

func BenchmarkBlit32(b *testing.B) {
	d := NewTestDevice(800, 480, 32, false)
	img := NewTestImage(800, 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Blit(img)
	}
}

func BenchmarkBlit32Rotate(b *testing.B) {
	d := NewTestDevice(800, 480, 32, true)
	img := NewTestImage(800, 480)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Blit(img)
	}
}
