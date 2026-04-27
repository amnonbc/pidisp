package drm

import (
	"image/color"
	"testing"
)

// TestFBBridge verifies that the DRM package can create test images
// without importing the parent pidisp package (avoiding import cycles).
func TestFBBridge(t *testing.T) {
	img := newTestImage(100, 100)
	if img == nil {
		t.Fatal("newTestImage returned nil")
	}

	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 100 {
		t.Errorf("Expected 100x100 image, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}

	// Verify the test pattern
	rgba := img.RGBAAt(50, 50)
	if rgba.R != 50 || rgba.G != 50 || rgba.B != 100 || rgba.A != 255 {
		t.Errorf("Unexpected color at (50,50): %+v", rgba)
	}
}

// TestImageCreation ensures our local test image function works correctly
func TestImageCreation(t *testing.T) {
	img := newTestImage(16, 16)

	// Test corner pixels
	tests := []struct {
		x, y int
		want color.RGBA
	}{
		{0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255}},
		{15, 0, color.RGBA{R: 15, G: 0, B: 15, A: 255}},
		{0, 15, color.RGBA{R: 0, G: 15, B: 15, A: 255}},
		{15, 15, color.RGBA{R: 15, G: 15, B: 30, A: 255}},
	}

	for _, tc := range tests {
		got := img.RGBAAt(tc.x, tc.y)
		if got != tc.want {
			t.Errorf("pixel (%d,%d): got %+v, want %+v", tc.x, tc.y, got, tc.want)
		}
	}
}
